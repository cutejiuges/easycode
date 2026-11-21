package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/extension/hook"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool/builtin"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

const preCompactFixtureFocus = "hook emphasis marker"

type compactionFixtureRunner struct {
	t          *testing.T
	runs       int
	blockFirst bool
	automatic  bool
	post       bool
	postRuns   int
}

func (*compactionFixtureRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }

func (runner *compactionFixtureRunner) Run(_ context.Context, id domain.HookRunIdentity, declaration domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
	runner.runs++
	stdin, err := input.Bytes()
	if id.Event == domain.HookPostCompact {
		runner.postRuns++
		want := `"compact_summary":"answer-2"`
		if runner.blockFirst {
			want = `"compact_summary":"short handoff"`
		}
		if runner.automatic && (!bytes.Contains(stdin, []byte(`"trigger":"auto"`)) ||
			!bytes.Contains(stdin, []byte(`"sample_index":0`)) || bytes.Contains(stdin, []byte("new-exact-correction"))) {
			runner.t.Error("PostCompact leaked a pending input or lost the automatic scope")
		}
		if err != nil || !bytes.Contains(stdin, []byte(want)) || bytes.Contains(stdin, []byte("archived task details")) ||
			bytes.Contains(stdin, []byte("custom_instructions")) || bytes.Contains(stdin, []byte("encrypted_content")) ||
			bytes.Contains(stdin, []byte("test signature")) || bytes.Contains(stdin, []byte("task_progress")) {
			runner.t.Error("PostCompact did not receive just the installed semantic summary")
		}
		return domain.HookExecution{Result: domain.HookRunResult{
			Identity: id, Status: domain.HookError, Code: domain.HookExitFailed, Origin: "execution",
			ExitKnown: true, ExitCode: 2, CleanupConfirmed: true, StderrBytes: 7,
		}}
	}
	trigger, instructions := `"trigger":"manual"`, `"custom_instructions":"preserve unfinished tests"`
	if runner.automatic {
		trigger, instructions = `"trigger":"auto"`, `"custom_instructions":null`
	}
	if err != nil || !bytes.Contains(stdin, []byte(instructions)) || !bytes.Contains(stdin, []byte(trigger)) || bytes.Contains(stdin, []byte("archived task details")) || bytes.Contains(stdin, []byte("new-exact-correction")) {
		runner.t.Error("hook stdin lost or leaked compaction input")
	}
	stdout := []byte(`{"hookSpecificOutput":{"hookEventName":"PreCompact","summaryInstructions":"` + preCompactFixtureFocus + `"}}`)
	if runner.blockFirst && runner.runs == 1 {
		stdout = []byte(`{"continue":false}`)
	}
	parsed := hook.ParseExecution(id.Event, declaration.Reference.Feedback, 0, stdout, domain.HookRewriteContext{})
	return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: parsed.Status, Code: parsed.Code, Origin: "execution", ExitKnown: true, CleanupConfirmed: true, StdoutBytes: int64(len(stdout))}, Feedback: parsed.Feedback}
}

func TestPreCompactOpenAIHandshakeFallbackKeepsOneGateDecision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var handshakes, summaries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			handshakes.Add(1)
			w.WriteHeader(http.StatusUpgradeRequired)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte(`"tool_choice":"none"`)) {
			summaries.Add(1)
			if !bytes.Contains(body, []byte(`preserve unfinished tests\n\n`+preCompactFixtureFocus)) || bytes.Contains(body, []byte(`"previous_response_id"`)) {
				t.Error("fallback did not use the approved full summary body")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		serveStopAssistant(w, domain.ProviderOpenAI, "short handoff")
	}))
	defer server.Close()
	root, workspace := filepath.Join(t.TempDir(), "sessions"), t.TempDir()
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture")}}
	seed, err := openChatResources(ctx, cfg, root, "", workspace)
	if err != nil {
		t.Fatal(err)
	}
	submitAppTurn(t, seed, strings.Repeat("archived task details ", 1000))
	thread := seed.identity.ThreadID
	if err := seed.close(ctx); err != nil {
		t.Fatal(err)
	}
	cfg = cacheFixtureConfigMode(t, cfg, true, "websocket_auto")
	runner := &compactionFixtureRunner{t: t, blockFirst: true, post: true}
	res, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, compactionFixtureHooks(t, runner, "manual"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.close(context.Background())
	if err := res.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	for _, want := range []protocol.CompactionFailureCode{protocol.CompactionFailureHookDenied, ""} {
		result, err := res.session.CompactContext(ctx, "preserve unfinished tests")
		if err != nil || result.Status() != protocol.CommandStatusAccepted {
			t.Fatalf("compaction not accepted: %v", err)
		}
		var terminal bool
		for !terminal {
			select {
			case item, open := <-res.session.Outputs():
				if !open {
					t.Fatal("host closed before compaction terminal")
				}
				event, ok := item.ContextEvent()
				if !ok {
					continue
				}
				switch event.Kind {
				case protocol.EventHookTrustRequested:
					request, err := protocol.DecodeHookTrustRequested(event)
					if err != nil {
						t.Fatal(err)
					}
					if err := res.session.ResolveHookTrust(ctx, "", request, protocol.HookTrustSession); err != nil {
						t.Fatal(err)
					}
				case protocol.EventCompactionCompleted:
					if want != "" {
						t.Fatal("blocked summary completed")
					}
					terminal = true
				case protocol.EventCompactionFailed:
					failure, err := protocol.DecodeCompactionFailed(event)
					if want == "" || err != nil || failure.Code != want || !failure.Durable {
						t.Fatalf("unexpected compaction failure: %+v %v", failure, err)
					}
					terminal = true
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		waitCompactionIdle(t, res)
		if want != "" && (handshakes.Load() != 0 || summaries.Load() != 0) {
			t.Fatal("blocked compaction opened a summary connection")
		}
	}
	if runner.runs != 3 || runner.postRuns != 1 || handshakes.Load() != 1 || summaries.Load() != 1 {
		t.Fatalf("fallback duplicated a gate or summary: runs=%d handshakes=%d summaries=%d", runner.runs, handshakes.Load(), summaries.Load())
	}
}

func compactionFixtureHooks(t *testing.T, runner *compactionFixtureRunner, matcher string) chatResourceHooks {
	t.Helper()
	return chatResourceHooks{
		interactiveApprovals: true,
		loadHooks: func(context.Context, string, string) (domain.HookSnapshot, error) {
			if matcher == "" {
				return domain.NewHookSnapshot(nil, nil)
			}
			scope, err := domain.ParsePreCompactHookScope(matcher)
			if err != nil {
				return domain.HookSnapshot{}, err
			}
			declaration, err := domain.NewPreCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest([]byte(matcher)), "compact-focus", scope, "fixture", 5)
			if err != nil {
				return domain.HookSnapshot{}, err
			}
			declarations := []domain.HookDeclaration{declaration}
			if runner.post {
				post, err := domain.NewPostCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, declaration.Reference.Revision, "observe-compact", scope, "fixture", 5)
				if err != nil {
					return domain.HookSnapshot{}, err
				}
				declarations = append(declarations, post)
			}
			return domain.NewHookSnapshot(declarations, nil)
		},
		newHookRunner: func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
			return runner, nil
		},
	}
}

func TestManualCompactionProvidersResumeEquivalent(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				requests = append(requests, body)
				mu.Unlock()
				compact := bytes.Contains(body, []byte(`"tool_choice"`))
				assertReasoningRequest(t, family, body, compact)
				answer := 1
				if compact {
					answer = 2
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if compact {
					wire := activeReasoningFixture(family, answer, false)
					if family == domain.ProviderAnthropic {
						wire = bytes.Replace(wire, []byte(`data: {"type":"message_stop"}`), []byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}"), 1)
					}
					_, _ = w.Write(wire)
					return
				}

				if family == domain.ProviderOpenAI {
					writeOpenAIAppTurn(w, answer)
				} else {
					var data bytes.Buffer
					writeAnthropicAppTurn(&data, answer)
					wire := strings.Replace(data.String(), `data: {"type":"message_stop"}`, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}", 1)
					_, _ = io.WriteString(w, wire)
				}
			}))
			defer server.Close()
			budget, _ := contextplan.NewBudget(200000, 16384, 100)
			cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("compaction-secret"), Model: "claude-test"}, ContextBudget: budget}
			cfg = reasoningFixtureConfig(t, cfg)
			var nextRequests [][]byte
			var nextObservations []protocol.CacheObservationPayload
			for _, scenario := range []struct {
				matcher string
				resume  bool
			}{{}, {matcher: "auto"}, {matcher: "manual"}, {matcher: "manual", resume: true}} {
				root, cwd := filepath.Join(t.TempDir(), "sessions"), t.TempDir()
				paths := dataPaths{sessionRoot: root}
				runner := &compactionFixtureRunner{t: t, post: scenario.matcher == "manual"}
				resources, err := openChatResourcesWithHooks(context.Background(), cfg, paths, "", false, cwd, compactionFixtureHooks(t, runner, scenario.matcher))
				if err != nil {
					t.Fatal(err)
				}
				if err := resources.session.StartInteractive(context.Background()); err != nil {
					t.Fatal(err)
				}
				events, err := submitInteractiveFixture(t, resources.session, context.Background(), strings.Repeat("archived task details ", 1000))
				if err != nil {
					t.Fatal(err)
				}
				for event := range events {
					if event.Kind == protocol.EventTurnFailed {
						t.Fatalf("initial turn failed: %s", event.Payload)
					}
				}
				waitCompactionIdle(t, resources)
				var ordinaryFingerprint string
				if runner.post {
					queryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					baseline := queryFixtureCacheSnapshot(t, queryCtx, resources, "pre-compaction-cache")
					cancel()
					if baseline.LastSample == nil {
						t.Fatal("missing ordinary sampling baseline")
					}
					ordinaryFingerprint = baseline.LastSample.NativeFingerprint
				}
				var model tea.Model = tui.NewModel(context.Background(), "test", resources.session)
				model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
				model, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/compact preserve unfinished tests")})
				var command tea.Cmd
				model, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
				if command == nil {
					t.Fatal("TUI did not dispatch compact")
				}
				model, _ = model.Update(command())
				var result protocol.CommandResult
				for item := range resources.session.Outputs() {
					model, _ = model.Update(item)
					if accepted, ok := item.CommandResult(); ok && accepted.CommandKind() == protocol.CommandCompactContext {
						result = accepted
						break
					}
				}
				if result.Status() != protocol.CommandStatusAccepted {
					t.Fatal("TUI compaction not accepted")
				}
				started, completed, closed := 0, 0, 0
				var checkpointSequence uint64
				var installedRevision uint64
				for item := range resources.session.Outputs() {
					if event, ok := item.TurnEvent(); ok && event.Kind == protocol.EventAssistantReasoning {
						t.Fatal("compaction reasoning entered ordinary turn")
					}
					model, _ = model.Update(item)
					event, ok := item.ContextEvent()
					if !ok {
						continue
					}
					if event.CompactionID != result.CompactionID() {
						t.Fatal("compaction correlation lost")
					}
					if event.Kind == protocol.EventCompactionStarted {
						started++
					}
					if event.Kind == protocol.EventPostCompactInstalled {
						phase, err := protocol.DecodePostCompactInstalled(event)
						if err != nil || phase.Count != 1 || !runner.post || runner.postRuns != 0 {
							t.Fatal("invalid post install phase", err)
						}
						checkpointSequence = phase.CheckpointSequence
						installedRevision = phase.Revision
					}
					if event.Kind == protocol.EventHookTrustRequested {
						request, err := protocol.DecodeHookTrustRequested(event)
						if err != nil {
							t.Fatal(err)
						}
						if request.Declaration.Reference.Event == domain.HookPostCompact {
							cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							cache := queryFixtureCacheSnapshot(t, cacheCtx, resources, "post-compact-review-cache")
							assertInstalledContext(t, cacheCtx, resources, installedRevision)
							cancel()
							if cache.LastSummary == nil || cache.LastSummary.CompactionID != event.CompactionID ||
								cache.LastSample == nil || cache.LastSample.NativeFingerprint != ordinaryFingerprint ||
								cache.Activity != "compacting" || checkpointSequence == 0 {
								t.Fatal("post review did not observe committed cache and original sample baseline")
							}
						}
						if err := resources.session.ResolveHookTrust(context.Background(), event.TurnID, request, protocol.HookTrustSession); err != nil {
							t.Fatal(err)
						}
					}
					if event.Kind == protocol.EventPostCompactClosed {
						result, err := protocol.DecodePostCompactClosed(event)
						if err != nil || result.State != protocol.PostCompactFailed || result.CheckpointSequence != checkpointSequence ||
							result.Code != domain.HookExitFailed || result.Finished != 1 {
							t.Fatalf("unexpected post close: %+v %v", result, err)
						}
						closed++
					}
					if event.Kind == protocol.EventCompactionFailed {
						t.Fatalf("compaction failed: %s", event.Payload)
					}
					if event.Kind == protocol.EventCompactionCompleted {
						completed++
						break
					}
				}
				if started != 1 || completed != 1 || (closed == 1) != runner.post {
					t.Fatal("compaction lifecycle duplicated")
				}
				if !strings.Contains(model.View(), "Conversation compacted") {
					t.Fatal("TUI completion boundary missing")
				}
				waitCompactionIdle(t, resources)
				page, err := resources.transcriptQueries.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail, ThreadID: resources.identity.ThreadID, Limit: 50})
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Items) != 3 || page.Items[0].Kind != domain.TranscriptCompaction || page.Items[1].Kind != domain.TranscriptAssistant || page.Items[2].Kind != domain.TranscriptUser {
					t.Fatalf("full transcript lost: %#v", page.Items)
				}
				body, err := resources.transcriptQueries.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: resources.identity.ThreadID, Cursor: page.Items[0].Cursor, Source: protocol.TranscriptText, Limit: 1024})
				if err != nil || body.Text != "answer-2" {
					t.Fatalf("summary detail=%q err=%v", body.Text, err)
				}
				thread := resources.identity.ThreadID
				journalPath, err := resources.service.repository.JournalPath(thread)
				if err != nil {
					t.Fatal(err)
				}
				if scenario.resume {
					if err := resources.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					journal, err := os.ReadFile(journalPath)
					if err != nil {
						t.Fatal(err)
					}
					writeJournalPrefix(t, journalPath, journal, checkpointSequence+1)
					resources, err = openChatResourcesWithHooks(context.Background(), cfg, paths, string(thread), false, cwd, compactionFixtureHooks(t, runner, scenario.matcher))
					if err != nil {
						t.Fatal(err)
					}
					if len(resources.history.Turns) != 1 || !strings.Contains(resources.history.Turns[0].Messages[0].Text, "archived task details") {
						t.Fatal("resume lost original user input")
					}
					if err := resources.session.StartInteractive(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				events, err = submitInteractiveFixture(t, resources.session, context.Background(), "continue explicitly")
				if err != nil {
					t.Fatal(err)
				}
				for event := range events {
					if event.Kind == protocol.EventTurnFailed {
						t.Fatalf("continuation failed: %s", event.Payload)
					}
				}
				cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				cache := completionCacheSnapshot(t, cacheCtx, resources)
				cancel()
				if cache.LastSample == nil {
					t.Fatal("continuation has no canonical request observation")
				}
				nextObservations = append(nextObservations, *cache.LastSample)
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				wantRuns := 0
				if scenario.matcher == "manual" {
					wantRuns = 2
				}
				if runner.runs != wantRuns {
					t.Fatalf("unexpected executions: %d", runner.runs)
				}
				if scenario.resume {
					loaded, plan := loadToolLoopPlan(t, root, thread)
					if len(plan.Compactions) != 1 || plan.Compactions[0].TerminalSequence != checkpointSequence || plan.InterruptedPostCompact != nil ||
						loaded.Records[checkpointSequence+1].EventKind != session.EventPostCompactHookClosed {
						t.Fatal("resume did not close the observed prefix at the original checkpoint")
					}
					closed, err := session.DecodePostCompactHookClosedPayload(loaded.Records[checkpointSequence+1])
					if err != nil || closed.State != protocol.PostCompactUncertain || closed.Finished != 0 {
						t.Fatalf("resume invented a result for the lone admission: %+v %v", closed, err)
					}
				}
				journal, err := os.ReadFile(journalPath)
				if err != nil || bytes.Contains(journal, []byte(preCompactFixtureFocus)) || bytes.Contains(journal, []byte("preserve unfinished tests")) {
					t.Fatalf("focus persisted in journal or journal unavailable: %v", err)
				}
				mu.Lock()
				nextRequests = append(nextRequests, append([]byte{}, requests[len(requests)-1]...))
				mu.Unlock()
			}
			mu.Lock()
			count := len(requests)
			captured := append([][]byte{}, requests...)
			mu.Unlock()
			if count != 12 {
				t.Fatalf("unexpected automatic continuation or regeneration: %d requests", count)
			}
			if !bytes.Equal(nextRequests[0], nextRequests[2]) || !bytes.Equal(nextRequests[2], nextRequests[3]) {
				t.Fatal("restored request differs from uninterrupted request")
			}
			if nextObservations[0].RequestFingerprint != nextObservations[2].RequestFingerprint || nextObservations[2].RequestFingerprint != nextObservations[3].RequestFingerprint ||
				nextObservations[0].NativeFingerprint != nextObservations[2].NativeFingerprint || nextObservations[2].NativeFingerprint != nextObservations[3].NativeFingerprint {
				t.Fatal("restored fingerprints differ from uninterrupted request")
			}
			if bytes.Contains(nextRequests[2], []byte("archived task details")) || bytes.Contains(nextRequests[2], []byte("preserve unfinished tests")) || bytes.Contains(nextRequests[2], []byte(preCompactFixtureFocus)) || !bytes.Contains(nextRequests[2], []byte("answer-2")) {
				t.Fatal("active window did not replace archived input")
			}
			if !bytes.Equal(captured[1], captured[4]) {
				t.Fatal("nonmatching declaration changed the summary request")
			}
			for _, index := range []int{1, 4, 7, 10} {
				if !bytes.Contains(captured[index], []byte("preserve unfinished tests")) || !bytes.Contains(captured[index], []byte("archived task details")) {
					t.Fatal("summary request lost source or instructions")
				}
			}
			focus := []byte(`\n\n` + preCompactFixtureFocus)
			for _, index := range []int{7, 10} {
				if !bytes.Equal(bytes.Replace(captured[index], focus, nil, 1), captured[1]) {
					t.Fatal("checked summary altered data outside the authorized focus")
				}
			}
		})
	}
}

func assertInstalledContext(t *testing.T, ctx context.Context, resources *chatResources, revision uint64) {
	t.Helper()
	if _, err := resources.session.QueryContext(ctx, "post-compact-review-context"); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case item := <-resources.session.Outputs():
			if event, ok := item.ContextEvent(); ok && event.Kind == protocol.EventContextSnapshot {
				context, err := protocol.DecodeContextSnapshot(event)
				if err != nil || context.Revision != revision || context.Activity != "compacting" {
					t.Fatalf("review observed the wrong context revision or activity: %+v %v", context, err)
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

// waitCompactionIdle 通过真实只读控制快照等待 owner 收到 worker 清理完成，不猜测调度时序。
func waitCompactionIdle(t *testing.T, resources *chatResources) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		requestID, err := domain.GenerateInputID()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := resources.session.QueryContext(ctx, protocol.RequestID("test-context-"+string(requestID))); err != nil {
			t.Fatal(err)
		}
		for {
			select {
			case item := <-resources.session.Outputs():
				event, ok := item.ContextEvent()
				if !ok || event.Kind != protocol.EventContextSnapshot {
					continue
				}
				snapshot, err := protocol.DecodeContextSnapshot(event)
				if err != nil {
					t.Fatal(err)
				}
				if snapshot.Activity == "idle" {
					return
				}
			case <-ctx.Done():
				t.Fatal("owner did not finish cleanup")
			}
			break
		}
	}
}

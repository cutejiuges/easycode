//go:build darwin

package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool/builtin"
)

func TestSessionStartRealRunnerBothProvidersAndCheckpointRestore(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		for _, trigger := range []string{"manual", "auto"} {
			t.Run(string(family)+"/"+trigger, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer cancel()
				workspace, root := t.TempDir(), filepath.Join(t.TempDir(), "sessions")
				writeSessionStartFixture(t, workspace, false)
				var mu sync.Mutex
				var requests [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					body, err := io.ReadAll(request.Body)
					if err != nil {
						t.Error(err)
					}
					mu.Lock()
					requests = append(requests, body)
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					text := "ordinary-answer"
					if bytes.Contains(body, []byte(`"tool_choice":"none"`)) || bytes.Contains(body, []byte(`"tool_choice":{"type":"none"}`)) {
						text = "independent-summary"
					}
					serveStopAssistant(w, family, text)
				}))
				defer server.Close()
				cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("private-session-start-key")}}
				if trigger == "auto" {
					cfg.ContextBudget, _ = contextplan.NewBudget(22000, 0, 0)
				}
				cfg = cacheFixtureConfig(t, cfg, true)
				open := func(thread string, interactive bool) *chatResources {
					resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, thread, false, workspace, chatResourceHooks{interactiveApprovals: interactive})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = resources.close(context.Background()) })
					if interactive {
						if err := resources.session.StartInteractive(ctx); err != nil {
							t.Fatal(err)
						}
					}
					return resources
				}
				close := func(resources *chatResources) {
					if err := resources.close(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				first := open("", true)
				thread := first.identity.ThreadID
				journalPath, err := first.service.repository.JournalPath(thread)
				if err != nil {
					t.Fatal(err)
				}
				if got := runSessionStartFixtureTurn(t, ctx, first, "first-user "+strings.Repeat("substantial history ", 2850)); !slices.Equal(got, []domain.SessionStartSource{domain.SessionStartStartup}) {
					t.Fatal("new session used wrong source", got)
				}
				close(first)
				resumed := open(string(thread), true)
				if trigger == "manual" {
					result, err := resumed.session.CompactContext(ctx, "")
					if err != nil || result.Status() != protocol.CommandStatusAccepted {
						t.Fatal("manual compaction admission", result.Status(), err)
					}
					got, postClosed := sessionStartFixtureEvents(t, ctx, resumed, protocol.EventCompactionCompleted)
					if !postClosed || !slices.Equal(got, []domain.SessionStartSource{domain.SessionStartCompact}) {
						t.Fatal("manual compaction lost post-then-compact ordering", got, postClosed)
					}
					close(resumed)
					resumed = open(string(thread), true)
				}
				want := []domain.SessionStartSource{domain.SessionStartResume}
				if trigger == "auto" {
					want = []domain.SessionStartSource{domain.SessionStartCompact, domain.SessionStartResume}
				}
				if got := runSessionStartFixtureTurn(t, ctx, resumed, "resumed-exact-user "+strings.Repeat("correction ", 500)); !slices.Equal(got, want) {
					t.Fatal("wrong compact/resume source order", got)
				}
				close(resumed)
				mu.Lock()
				captured := slices.Clone(requests)
				mu.Unlock()
				if len(captured) != 3 || !bytes.Contains(captured[0], []byte("context-startup")) || !bytes.Contains(captured[1], []byte("context-startup")) || bytes.Contains(captured[1], []byte("context-compact")) || bytes.Contains(captured[1], []byte("resumed-exact-user")) {
					t.Fatal("unexpected first sample or old-window summary", len(captured))
				}
				continuation := captured[2]
				summary, compact, resume, realInput := bytes.Index(continuation, []byte("independent-summary")), bytes.Index(continuation, []byte("context-compact")), bytes.Index(continuation, []byte("context-resume")), bytes.Index(continuation, []byte("resumed-exact-user"))
				if summary < 0 || summary >= compact || compact >= resume || resume >= realInput || bytes.Contains(continuation, []byte("context-startup")) || bytes.Count(continuation, []byte("context-compact")) != 1 {
					t.Fatal("wrong native continuation order", summary, compact, resume, realInput)
				}
				before, err := os.ReadFile(journalPath)
				if err != nil || bytes.Contains(before, []byte("RAW-SESSION-START-STDERR")) || bytes.Contains(before, []byte("private-session-start-key")) {
					t.Fatal("unsafe saved session", err)
				}
				_, plan := loadToolLoopPlan(t, root, thread)
				if len(plan.Compactions) != 1 {
					t.Fatal("expected one verified checkpoint", len(plan.Compactions))
				}
				headless := open(string(thread), false)
				close(headless)
				after, err := os.ReadFile(journalPath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("offline restore changed completed facts", err)
				}
				mu.Lock()
				count := len(requests)
				mu.Unlock()
				if count != len(captured) {
					t.Fatal("restoring context dispatched a provider request")
				}
			})
		}
	}
}

func TestSessionStartBranchUsesDeferredResumeContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	workspace, home := privateAppTempDir(t), privateAppTempDir(t)
	writeSessionStartFixture(t, workspace, false)
	requests := make(chan []byte, 7)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		select {
		case requests <- body:
		default:
			t.Error("unexpected model request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		answer := "fixture-answer"
		if bytes.Contains(body, []byte(`"tool_choice":"none"`)) {
			answer = "retained-manual-summary"
			if bytes.Contains(body, []byte("retained-followup")) {
				answer = "excluded-auto-summary"
			}
		}
		serveStopAssistant(w, domain.ProviderOpenAI, answer)
	}))
	defer server.Close()
	cfg := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("branch-context-fixture"),
	}}
	cfg.ContextBudget, _ = contextplan.NewBudget(22000, 0, 0)
	paths := dataPaths{sessionRoot: filepath.Join(home, "sessions"), catalogPath: filepath.Join(home, "catalog.json")}
	owner, source, candidates := startBranchFixture(t, ctx, cfg, paths, workspace)
	if got := runSessionStartFixtureTurn(t, ctx, source, "source prompt "+strings.Repeat("foundational history ", 900)); !slices.Equal(got, []domain.SessionStartSource{domain.SessionStartStartup}) {
		t.Fatal("source did not commit startup context", got)
	}
	waitCompactionIdle(t, source)
	if result, err := source.session.CompactContext(ctx, ""); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("manual checkpoint admission failed", err)
	}
	if got, _ := sessionStartFixtureEvents(t, ctx, source, protocol.EventCompactionCompleted); !slices.Equal(got, []domain.SessionStartSource{domain.SessionStartCompact}) {
		t.Fatal("manual checkpoint did not commit compact context", got)
	}
	waitCompactionIdle(t, source)
	if got := runSessionStartFixtureTurn(t, ctx, source, "retained-followup "+strings.Repeat("retained detail ", 500)); len(got) != 0 {
		t.Fatal("retained follow-up unexpectedly compacted", got)
	}
	waitCompactionIdle(t, source)
	sourcePath, err := source.service.repository.JournalPath(source.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	retained := readSwitchJournal(t, sourcePath)
	selected := "excluded-rewind-prompt " + strings.Repeat("substantial history ", 2850)
	if got := runSessionStartFixtureTurn(t, ctx, source, selected); !slices.Equal(got, []domain.SessionStartSource{domain.SessionStartCompact}) {
		t.Fatal("selected turn did not commit automatic checkpoint context", got)
	}
	waitCompactionIdle(t, source)
	durable := readSwitchJournal(t, sourcePath)
	if !bytes.Contains(durable[len(retained):], []byte("excluded-auto-summary")) {
		t.Fatal("selected turn did not include the excluded automatic checkpoint")
	}
	branch := activateHistoricalBranchFixture(t, ctx, owner, source, candidates, selected)
	path, err := branch.service.repository.JournalPath(branch.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	seed := readSwitchJournal(t, path)
	marker := switchJournalRecord(t, seed, false)
	if len(requests) != 5 || marker.EventKind != session.EventThreadForked ||
		marker.Sequence != switchJournalRecord(t, retained, false).Sequence+1 ||
		!bytes.HasPrefix(seed, retained) || bytes.Contains(seed, []byte("excluded-auto-summary")) ||
		!bytes.Equal(readSwitchJournal(t, sourcePath), durable) {
		t.Fatal("branch did not retain precisely the closed scope before selected turn")
	}
	for range 5 {
		<-requests
	}
	copyRoot, _ := installThreadFixture(t, seed, branch.identity.ThreadID)
	cold, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: copyRoot}, string(branch.identity.ThreadID), false, workspace, chatResourceHooks{interactiveApprovals: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeResumeTestResources(t, cold)
	if err := cold.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	for _, chat := range []*chatResources{branch, cold} {
		if got := runSessionStartFixtureTurn(t, ctx, chat, "branch prompt"); !slices.Equal(got, []domain.SessionStartSource{domain.SessionStartResume}) {
			t.Fatal("branch did not independently authorize resume context", got)
		}
		waitCompactionIdle(t, chat)
	}
	if len(requests) != 2 {
		t.Fatalf("wanted two model requests, got %d", len(requests))
	}
	live, reopened := <-requests, <-requests
	if !bytes.Equal(live, reopened) || bytes.Count(live, []byte("context-compact")) != 1 ||
		!bytes.Contains(live, []byte("retained-manual-summary")) || bytes.Contains(live, []byte("excluded-auto-summary")) ||
		bytes.Contains(live, []byte("excluded-rewind-prompt")) ||
		bytes.Count(live, []byte("context-resume")) != 1 || !bytes.Contains(live, []byte("branch prompt")) {
		t.Fatal("branch continuation changed inherited or newly authorized context")
	}
}

func writeSessionStartFixture(t *testing.T, workspace string, stop bool) {
	t.Helper()
	dir := filepath.Join(workspace, ".easycode")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	command := `input=$(cat); printf RAW-SESSION-START-STDERR >&2; case "$input" in *'"source":"startup"'*) source=startup ;; *'"source":"resume"'*) source=resume ;; *'"source":"compact"'*) source=compact ;; *) exit 2 ;; esac; printf '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"context-%s"}}' "$source"`
	hooks := map[string]any{
		"SessionStart": []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"id": "context", "type": "command", "command": command, "feedback": "model"}}}},
		"PostCompact":  []any{map[string]any{"matcher": "manual", "hooks": []any{map[string]any{"id": "observer", "type": "command", "command": "cat >/dev/null; exit 7"}}}},
	}
	if stop {
		stopCommand := `input=$(cat); case "$input" in *'"sample_index":0'*) printf '%s' '{"decision":"block","reason":"stop-feedback-exact"}' ;; *) printf '{}' ;; esac`
		hooks["Stop"] = []any{map[string]any{"hooks": []any{map[string]any{"id": "completion", "type": "command", "command": stopCommand, "feedback": "model"}}}}
	}
	raw, err := codec.MarshalStable(map[string]any{"hooks": hooks})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func runSessionStartFixtureTurn(t *testing.T, ctx context.Context, resources *chatResources, text string) []domain.SessionStartSource {
	t.Helper()
	if result, err := appendInteractiveText(ctx, resources.session, text); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("input admission", result.Status(), err)
	}
	got, _ := sessionStartFixtureEvents(t, ctx, resources, protocol.EventTurnCompleted)
	return got
}

func sessionStartFixtureEvents(t *testing.T, ctx context.Context, resources *chatResources, terminal protocol.EventKind) ([]domain.SessionStartSource, bool) {
	t.Helper()
	var sources []domain.SessionStartSource
	var postClosed bool
	for {
		select {
		case item, open := <-resources.session.Outputs():
			if !open {
				t.Fatal("control closed before terminal")
			}
			event, ok := item.TurnEvent()
			if !ok {
				event, ok = item.ContextEvent()
			}
			if !ok {
				continue
			}
			switch event.Kind {
			case protocol.EventHookTrustRequested:
				request, err := protocol.DecodeHookTrustRequested(event)
				if err != nil {
					t.Fatal(err)
				}
				if err := resources.session.ResolveHookTrust(ctx, event.TurnID, request, protocol.HookTrustSession); err != nil {
					t.Fatal(err)
				}
			case protocol.EventPostCompactClosed:
				postClosed = true
			case protocol.EventSessionStart:
				payload, err := protocol.DecodeSessionStart(event)
				if err != nil {
					t.Fatal(err)
				}
				if payload.State == protocol.SessionStartApplied {
					sources = append(sources, payload.Target.Source)
					if payload.Target.Source == domain.SessionStartCompact && payload.Target.Scope.Trigger == domain.CompactionManual && !postClosed {
						t.Fatal("compact context applied before post observation closed")
					}
				}
			case protocol.EventTurnFailed, protocol.EventCompactionFailed:
				t.Fatalf("operation failed: %s", event.Payload)
			}
			if event.Kind == terminal {
				return sources, postClosed
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestSessionStartCompactPrecedesFrozenStopContinuation(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			workspace, root := t.TempDir(), filepath.Join(t.TempDir(), "sessions")
			writeSessionStartFixture(t, workspace, true)
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				mu.Lock()
				requests = append(requests, body)
				index := len(requests)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				text := "finished-answer"
				if index == 1 {
					text = "old-stop-candidate " + strings.Repeat("long partial solution ", 2600)
				} else if bytes.Contains(body, []byte(`"tool_choice":"none"`)) || bytes.Contains(body, []byte(`"tool_choice":{"type":"none"}`)) {
					text = "compact-summary-exact"
				}
				serveStopAssistant(w, family, text)
			}))
			defer server.Close()
			budget, _ := contextplan.NewBudget(22000, 0, 0)
			cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("private-session-start-key")}, ContextBudget: budget}
			resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			defer resources.close(context.Background())
			if err := resources.session.StartInteractive(ctx); err != nil {
				t.Fatal(err)
			}
			if got := runSessionStartFixtureTurn(t, ctx, resources, "initial-stop-user"); !slices.Equal(got, []domain.SessionStartSource{domain.SessionStartStartup, domain.SessionStartCompact}) {
				t.Fatal("wrong automatic compact boundary", got)
			}
			thread := resources.identity.ThreadID
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			captured := slices.Clone(requests)
			mu.Unlock()
			if len(captured) != 3 || !bytes.Contains(captured[1], []byte("old-stop-candidate")) || bytes.Contains(captured[1], []byte("stop-feedback-exact")) {
				t.Fatal("summary did not use only the prior window", len(captured))
			}
			final := captured[2]
			summary, compact, feedback := bytes.Index(final, []byte("compact-summary-exact")), bytes.Index(final, []byte("context-compact")), bytes.Index(final, []byte("stop-feedback-exact"))
			if summary < 0 || summary >= compact || compact >= feedback || bytes.Contains(final, []byte("old-stop-candidate")) {
				t.Fatal("lost ordered Stop continuation", summary, compact, feedback)
			}
			loaded, plan := loadToolLoopPlan(t, root, thread)
			if len(plan.Stops) != 2 || len(plan.Compactions) != 1 || plan.Stops[0].DeliverySequence == 0 || plan.Stops[1].Plan.Sample().SampleIndex != 1 || plan.Stops[1].Plan.Sample().TurnID != plan.Stops[0].Plan.Sample().TurnID {
				t.Fatal("Stop did not preserve its sample identity")
			}
			assertInterruptedCompactSessionStartBeforeStop(t, ctx, cfg, root, workspace, thread, loaded, plan.Stops[0])
			mu.Lock()
			requestCount := len(requests)
			mu.Unlock()
			if requestCount != len(captured) {
				t.Fatal("recovery contacted the Provider", requestCount)
			}
		})
	}
}

func assertInterruptedCompactSessionStartBeforeStop(t *testing.T, ctx context.Context, cfg config.Config, root, workspace string, thread domain.ThreadID, loaded session.LoadResult, stop session.ReplayedStop) {
	t.Helper()
	var interrupted uint64
	for _, record := range loaded.Records {
		if record.EventKind != session.EventSessionStartHookStarted {
			continue
		}
		identity, err := session.DecodeSessionStartHookStarted(record)
		if err != nil {
			t.Fatal(err)
		}
		target, _ := identity.Target.SessionStart()
		if target.Source == domain.SessionStartCompact {
			interrupted = record.Sequence
			break
		}
	}
	feedback, ok := stop.Decision.Feedback()
	if interrupted == 0 || !ok || interrupted >= stop.DeliverySequence {
		t.Fatal("live journal did not contain an interrupted compact boundary before Stop delivery")
	}
	repo, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	path, err := repo.JournalPath(thread)
	_ = repo.Close()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	prefix := prefixLines(original, int(interrupted))
	resumeRoot, resumePath, _ := installPreviewFixture(t, prefix)
	runner := &appHookRunner{}
	var recovered []byte
	for range 2 {
		resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: resumeRoot}, string(thread), false, workspace, chatResourceHooks{
			newHookRunner: func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
				return runner, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := resources.close(ctx); err != nil {
			t.Fatal(err)
		}
		current, err := os.ReadFile(resumePath)
		if err != nil || !bytes.HasPrefix(current, prefix) || recovered != nil && !bytes.Equal(current, recovered) {
			t.Fatal("recovery rewrote committed bytes or repeated local compensation", err)
		}
		recovered = current
	}
	result, plan := loadToolLoopPlan(t, resumeRoot, thread)
	if runner.runs.Load() != 0 || len(result.Records) != int(interrupted)+3 || plan.StopRecovery != nil || plan.InterruptedSessionStart != nil {
		t.Fatal("recovery executed a Hook or left unfinished work")
	}
	closed, err := session.DecodeSessionStartHookClosed(result.Records[interrupted])
	latest := plan.NativeCommits[len(plan.NativeCommits)-1]
	if err != nil || closed.State != session.SessionStartUncertain || closed.Native != nil ||
		latest.Sequence != interrupted+2 || latest.Feedback == nil || *latest.Feedback != feedback ||
		result.Records[interrupted+2].EventKind != session.EventTurnFailed {
		t.Fatal("recovery did not close SessionStart before delivering the exact frozen Stop bytes", err)
	}
}

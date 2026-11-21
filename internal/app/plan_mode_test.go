package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/protocol"
	"easycode/internal/secret"
)

func TestPlanModeHeadlessKeepsExplicitSkills(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const skillBody = "PLAN_SKILL_BODY"
	workspace, emptyWorkspace := t.TempDir(), t.TempDir()
	skill := filepath.Join(workspace, ".easycode", "skills", "plan", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("---\ndescription: Plan fixture\n---\n"+skillBody+" $ARGUMENTS\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case bodies <- body:
		default:
			t.Error("unexpected extra headless request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(w, 1)
	}))
	defer server.Close()
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("plan-fixture")}}
	for _, scenario := range []struct {
		input, workspace, skillArgs string
		streaming                   bool
	}{
		{input: "/plan", workspace: emptyWorkspace},
		{input: "/plan off", workspace: workspace, skillArgs: "off"},
		{input: "/plan", workspace: workspace, streaming: true},
		{input: "$plan exact", workspace: workspace, skillArgs: "exact", streaming: true},
	} {
		resources, err := openChatResources(ctx, cfg, filepath.Join(t.TempDir(), "sessions"), "", scenario.workspace)
		if err != nil {
			t.Fatal(err)
		}
		if !scenario.streaming {
			if err := resources.session.Start(ctx); err != nil {
				t.Fatal(err)
			}
			result := headless.Run(ctx, resources.session, headless.RunConfig{Mode: headless.ModeText, Prompt: scenario.input,
				SessionID: resources.identity.SessionID, ThreadID: resources.identity.ThreadID, Output: io.Discard})
			if !result.Completed {
				t.Fatalf("headless once failed: %v", result.Failure)
			}
		} else {
			request, _ := json.Marshal(scenario.input)
			text := `{"version":1,"type":"input.submit","request_id":"plan-text","text":` + string(request) + "}\n"
			output := &appWriteCloser{}
			if outcome := runStreamJSON(ctx, Options{Input: io.NopCloser(strings.NewReader(text)), Output: output}, resources); outcome.Class != ExitSuccess {
				t.Fatalf("headless stream input: %#v %s", outcome, output.String())
			}
		}
		if err := resources.close(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case body := <-bodies:
			prepared := bytes.Contains(body, []byte(strings.TrimSpace(skillBody+" "+scenario.skillArgs)))
			if prepared != (scenario.workspace == workspace) || bytes.Contains(body, []byte(`"instructions":`)) ||
				!bytes.Contains(body, []byte(scenario.input)) || !bytes.Contains(body, []byte(`"name":"apply_patch"`)) {
				t.Fatalf("headless input changed mode or Skill dispatch: %q", scenario.input)
			}
		default:
			t.Fatalf("headless input did not reach the provider: %q", scenario.input)
		}
	}
}

func setAppWorkMode(t *testing.T, ctx context.Context, resources *chatResources, mode domain.WorkMode, id protocol.RequestID) {
	t.Helper()
	waitCompactionIdle(t, resources)
	result, err := resources.session.SetWorkMode(ctx, id, mode)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatalf("set mode %s: %v %v", mode, result, err)
	}
	confirmed, err := protocol.DecodeWorkModeSnapshot(waitSwitchObservation(t, ctx, resources, protocol.EventWorkModeSnapshot, id))
	if err != nil || confirmed.Mode != mode {
		t.Fatalf("wrong mode confirmation: %v %v", confirmed, err)
	}
}

func TestPlanModeHostDiagnostics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(requests.Add(1))
		if n == 2 {
			http.Error(w, "reject Plan request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(w, n)
	}))
	defer server.Close()
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("plan-fixture")}}
	resources, err := openChatResources(ctx, cfg, filepath.Join(t.TempDir(), "sessions"), "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	if err := resources.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	contextSources := func(id protocol.RequestID) []protocol.ContextSourcePayload {
		t.Helper()
		if result, err := resources.session.QueryContext(ctx, id); err != nil || result.Status() != protocol.CommandStatusAccepted {
			t.Fatalf("context query: %v %v", result, err)
		}
		payload, err := protocol.DecodeContextSnapshot(waitSwitchObservation(t, ctx, resources, protocol.EventContextSnapshot, id))
		if err != nil || payload.Activity != "idle" {
			t.Fatalf("context should describe the idle selection: %s %v", payload.Activity, err)
		}
		return payload.Sources[:2]
	}
	appEffortTurn(t, resources, "Ordinary baseline", false)
	waitCompactionIdle(t, resources)
	baseline := planModeCacheSnapshot(t, ctx, resources, "cache-default")
	ordinary := contextSources("context-default")
	setAppWorkMode(t, ctx, resources, domain.WorkModePlan, "select-plan")
	plan := contextSources("context-plan")
	got, err := plan[0].Estimate.Domain()
	if err != nil || got != estimate.String(domain.WorkModePlan.Instructions()) ||
		plan[1].Revision == ordinary[1].Revision {
		t.Fatalf("idle context did not update to Plan instructions and catalog: %v %v", plan, err)
	}
	assertOrdinaryPlanCacheBaseline(t, baseline, planModeCacheSnapshot(t, ctx, resources, "cache-pending"))
	appEffortTurn(t, resources, "Reject on upstream", true)
	waitCompactionIdle(t, resources)
	failed := planModeCacheSnapshot(t, ctx, resources, "cache-failed")
	assertOrdinaryPlanCacheBaseline(t, baseline, failed)
	if failed.LastFailure != "turn_failed" {
		t.Fatalf("cache did not preserve independent failure evidence: %v", failed.LastFailure)
	}
	appEffortTurn(t, resources, "Draft the plan successfully", false)
	waitCompactionIdle(t, resources)
	committed := planModeCacheSnapshot(t, ctx, resources, "cache-plan")
	if committed.LastSample == nil || baseline.LastSample == nil || committed.LastFailure != "" ||
		committed.LastSample.RequestFingerprint == baseline.LastSample.RequestFingerprint || committed.ChangedSource != "provider_profile" ||
		committed.Sources[0].Fingerprint == baseline.Sources[0].Fingerprint || committed.Sources[1].Fingerprint == baseline.Sources[1].Fingerprint ||
		committed.Sources[1].Revision != plan[1].Revision {
		t.Fatal("committed Plan request did not advance the cache baseline with both sources")
	}
	setAppWorkMode(t, ctx, resources, domain.WorkModeDefault, "select-default")
	reverted := contextSources("context-reverted")
	if reverted[0].Revision != ordinary[0].Revision || reverted[1].Revision != ordinary[1].Revision || requests.Load() != 3 {
		t.Fatal("restoring Default did not restore its sources or triggered work")
	}
	assertOrdinaryPlanCacheBaseline(t, committed, planModeCacheSnapshot(t, ctx, resources, "cache-reverted"))
}

func planModeCacheSnapshot(t *testing.T, ctx context.Context, resources *chatResources, id protocol.RequestID) protocol.CacheSnapshotPayload {
	t.Helper()
	if result, err := resources.session.QueryCache(ctx, id); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatalf("cache query: %v %v", result, err)
	}
	payload, err := protocol.DecodeCacheSnapshot(waitSwitchObservation(t, ctx, resources, protocol.EventCacheSnapshot, id))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertOrdinaryPlanCacheBaseline(t *testing.T, baseline, after protocol.CacheSnapshotPayload) {
	t.Helper()
	if baseline.LastSample == nil || after.LastSample == nil || after.LastSample.RequestFingerprint != baseline.LastSample.RequestFingerprint ||
		after.LastSample.NativeFingerprint != baseline.LastSample.NativeFingerprint || after.Sources != baseline.Sources ||
		after.ContentFingerprint != baseline.ContentFingerprint || after.StableFingerprint != baseline.StableFingerprint {
		t.Fatal("operation changed the prior ordinary cache baseline")
	}
}

func TestPlanModeCompactionAndRestore(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		family, scope := domain.ProviderOpenAI, "manual"
		if automatic {
			family, scope = domain.ProviderAnthropic, "auto"
		}
		t.Run(scope, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			const keyDecision = "PLAN_DECISION=keep exact spacing"
			const oldPlan = "## Reviewed plan\n\n" + keyDecision
			const implementation = "Implement with the agreed constraints:\n  " + keyDecision + "  "
			var count atomic.Int32
			bodies := make(chan []byte, 5)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				n := int(count.Add(1))
				select {
				case bodies <- body:
				default:
					t.Error("unexpected extra Provider request")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if family == domain.ProviderAnthropic && n == 1 {
					serveStopAssistant(w, family, oldPlan)
				} else if family == domain.ProviderAnthropic && n == 2 {
					serveStopAssistant(w, family, "short handoff")
				} else {
					serveWriteScenarioSample(w, family, n, nil)
				}
			}))
			defer server.Close()
			cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("plan-fixture")}}
			if automatic {
				cfg.ContextBudget, _ = contextplan.NewBudget(22000, 0, 0)
			}
			cfg = cacheFixtureConfig(t, cfg, true)
			root, workspace := filepath.Join(t.TempDir(), "sessions"), t.TempDir()
			runner := &compactionFixtureRunner{t: t, automatic: true}
			var hooks chatResourceHooks
			if automatic {
				hooks = compactionFixtureHooks(t, runner, scope)
			}
			live, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, "", false, workspace, hooks)
			if err != nil {
				t.Fatal(err)
			}
			defer live.close(context.Background())
			if err := live.session.StartInteractive(ctx); err != nil {
				t.Fatal(err)
			}
			setAppWorkMode(t, ctx, live, domain.WorkModePlan, "plan")
			if count.Load() != 0 {
				t.Fatal("mode selection made a request")
			}
			appEffortTurn(t, live, "Discuss "+keyDecision+" "+strings.Repeat("historical work ", 3500), false)
			promptField, toolDisabled := []byte(`"instructions":`), []byte(`"tool_choice":"none"`)
			if family == domain.ProviderAnthropic {
				promptField = []byte(`"system":`)
				toolDisabled = []byte(`"tool_choice":{"type":"none"}`)
			}
			first := <-bodies
			if !bytes.Contains(first, promptField) || bytes.Contains(first, []byte(`"name":"Write"`)) {
				t.Fatal("first Plan sample did not use its captured policy")
			}
			if automatic {
				setAppWorkMode(t, ctx, live, domain.WorkModeDefault, "exit-plan")
				if count.Load() != 1 {
					t.Fatal("exiting Plan submitted implementation")
				}
				submitCheckedAutoTurn(t, ctx, live, implementation)
			} else {
				waitCompactionIdle(t, live)
				before := planModeCacheSnapshot(t, ctx, live, "cache-before-summary")
				compactForSwitch(t, ctx, live)
				after := planModeCacheSnapshot(t, ctx, live, "cache-after-summary")
				assertOrdinaryPlanCacheBaseline(t, before, after)
				if after.LastSummary == nil || after.LastSummary.Purpose != domain.RequestSummary || !after.LastSummary.CompactionID.Valid() {
					t.Fatal("summary did not publish its independent cache observation")
				}
			}
			summary := <-bodies
			if bytes.Contains(summary, promptField) || !bytes.Contains(summary, []byte(`"name":"Write"`)) ||
				!bytes.Contains(summary, toolDisabled) || !bytes.Contains(summary, []byte(keyDecision)) ||
				bytes.Contains(summary, []byte("Implement with the agreed constraints")) {
				t.Fatal("summary mixed execution mode or pending input into its independent request")
			}
			if automatic {
				if runner.runs != 1 || !bytes.Contains(summary, []byte(preCompactFixtureFocus)) {
					t.Fatal("automatic summary did not use the trusted PreCompact result exactly once")
				}
				next := <-bodies
				quoted, _ := json.Marshal(implementation)
				if bytes.Contains(next, promptField) || !bytes.Contains(next, []byte(`"name":"Write"`)) || !bytes.Contains(next, quoted) || bytes.Count(next, []byte(keyDecision)) != 1 {
					t.Fatal("first implementation sample lost the explicit user text or used the old mode")
				}
				if count.Load() != 3 {
					t.Fatal("wrong number of requests during transition", count.Load())
				}
				assertCompactedPlanReadable(t, ctx, live, oldPlan)
				return
			}
			assertPlanCheckpointResume(t, ctx, cfg, live, workspace, bodies, &count)
		})
	}
}

func assertCompactedPlanReadable(t *testing.T, ctx context.Context, live *chatResources, expected string) {
	t.Helper()
	thread := live.identity.ThreadID
	page, err := live.transcriptQueries.ListTranscript(ctx, protocol.TranscriptListQuery{ThreadID: thread, Mode: protocol.TranscriptDetail, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.Kind != domain.TranscriptAssistant || !strings.Contains(item.Text, "Reviewed plan") {
			continue
		}
		body, err := live.transcriptQueries.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{
			ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptText, Limit: 1024,
		})
		if err != nil || body.Text != expected {
			t.Fatalf("archived Plan Markdown was altered: %q %v", body.Text, err)
		}
		return
	}
	t.Fatal("original Plan disappeared from the transcript after compaction")
}

func assertPlanCheckpointResume(t *testing.T, ctx context.Context, cfg config.Config, live *chatResources, workspace string, bodies <-chan []byte, count *atomic.Int32) {
	t.Helper()
	thread := live.identity.ThreadID
	journal, err := live.service.repository.JournalPath(thread)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := readSwitchJournal(t, journal)
	if bytes.Contains(checkpoint, []byte(`"work_mode"`)) || bytes.Contains(checkpoint, []byte(`"work_mode_snapshot"`)) {
		t.Fatal("process mode leaked into checkpoint")
	}
	restored, err := openChatResources(ctx, cfg, copyCompletionJournal(t, thread, checkpoint), string(thread), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close(context.Background())
	if err := restored.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	setAppWorkMode(t, ctx, restored, domain.WorkModePlan, "restore-plan")
	if count.Load() != 2 {
		t.Fatal("restore or mode re-selection made a request")
	}
	appEffortTurn(t, live, "Revise the compacted plan", false)
	appEffortTurn(t, restored, "Revise the compacted plan", false)
	first, second := <-bodies, <-bodies
	if !bytes.Equal(first, second) || !bytes.Contains(first, []byte(`"instructions":`)) || bytes.Contains(first, []byte(`"name":"Write"`)) {
		t.Fatal("post-compaction Plan request bytes changed across restore")
	}
	left := planModeCacheSnapshot(t, ctx, live, "cache-live")
	right := planModeCacheSnapshot(t, ctx, restored, "cache-restored")
	assertOrdinaryPlanCacheBaseline(t, left, right)
}

func compactForSwitch(t *testing.T, ctx context.Context, resources *chatResources) {
	t.Helper()
	result, err := resources.session.CompactContext(ctx, "preserve test facts")
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("target could not start compaction", err)
	}
	waitSwitchObservation(t, ctx, resources, protocol.EventCompactionCompleted, result.RequestID())
	waitCompactionIdle(t, resources)
}

func waitSwitchObservation(t *testing.T, ctx context.Context, resources *chatResources, kind protocol.EventKind, id protocol.RequestID) protocol.Event {
	t.Helper()
	for {
		select {
		case item, open := <-resources.session.Outputs():
			if !open {
				t.Fatal("control session closed before observation")
			}
			event, ok := item.ContextEvent()
			if ok && event.Kind == protocol.EventCompactionFailed {
				t.Fatalf("compaction failed: %s", event.Payload)
			}
			if ok && event.Kind == kind && item.RequestID() == id {
				return event
			}
		case <-ctx.Done():
			t.Fatalf("observation %s: %v", kind, ctx.Err())
		}
	}
}

func readSwitchJournal(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

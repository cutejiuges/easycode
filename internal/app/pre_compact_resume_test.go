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
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool/builtin"
)

type preCompactResumeRunner struct{ runs atomic.Int32 }

const pendingStopRecoveryFocus = "preserve the exact pending Stop continuation"

func (*preCompactResumeRunner) Preflight(context.Context) domain.HookCode { return domain.HookOK }

func (r *preCompactResumeRunner) Run(_ context.Context, identity domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
	r.runs.Add(1)
	result := domain.HookRunResult{Identity: identity, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
	var candidate domain.HookFeedbackCandidate
	if sample, ok := identity.Target.AssistantSample(); ok && sample.SampleIndex == 0 {
		result.Status, result.Code = domain.HookBlock, domain.HookDenied
		candidate, _ = domain.NewHookFeedbackCandidate(pendingStopRecoveryFocus)
	}
	return domain.HookExecution{Result: result, Feedback: candidate}
}

func TestPreCompactResumeClosesBeforePendingStopContinuation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		n := requests.Add(1)
		if n == 2 && (!bytes.Contains(body, []byte(`"tool_choice":"none"`)) || bytes.Contains(body, []byte(pendingStopRecoveryFocus))) {
			t.Error("pending Stop feedback entered the summary")
		}
		if n == 3 && !bytes.Contains(body, []byte(pendingStopRecoveryFocus)) {
			t.Error("frozen Stop feedback did not reach its ordinary continuation")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		serveWriteScenarioSample(w, domain.ProviderOpenAI, int(n), nil)
	}))
	defer server.Close()
	budget, err := contextplan.NewBudget(22000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "fixture-model"}, ContextBudget: budget}, false)
	root, workspace := filepath.Join(t.TempDir(), "sessions"), t.TempDir()
	scope, err := domain.ParsePreCompactHookScope("auto")
	if err != nil {
		t.Fatal(err)
	}
	revision := domain.HookDigest([]byte("compaction recovery fixture"))
	stop, err := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, revision, "verify", "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	check, err := domain.NewPreCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, revision, "retain", scope, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	load := func(context.Context, string, string) (domain.HookSnapshot, error) {
		return domain.NewHookSnapshot([]domain.HookDeclaration{stop, check}, nil)
	}
	runner := &preCompactResumeRunner{}
	makeRunner := func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
		return runner, nil
	}
	paths := dataPaths{sessionRoot: root}
	resources, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, workspace, chatResourceHooks{interactiveApprovals: true, loadHooks: load, newHookRunner: makeRunner})
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	control, err := startTUIControlSession(ctx, resources.runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Shutdown(context.Background())
	result, err := appendInteractiveText(ctx, control, strings.Repeat("historical work ", 3500))
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("submit recovery fixture", err)
	}
	for item := range control.Outputs() {
		event, ok := item.TurnEvent()
		if !ok {
			event, _ = item.ContextEvent()
		}
		if event.Kind == protocol.EventTurnFailed || event.Kind == protocol.EventCompactionFailed {
			t.Fatalf("prepare recovery fixture: %s", event.Payload)
		}
		if event.Kind == protocol.EventTurnCompleted {
			break
		}
		if event.Kind == protocol.EventHookTrustRequested {
			request, err := protocol.DecodeHookTrustRequested(event)
			if err != nil {
				t.Fatal(err)
			}
			if err := control.ResolveHookTrust(ctx, event.TurnID, request, protocol.HookTrustSession); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := control.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	thread := resources.identity.ThreadID
	journal, err := resources.service.repository.JournalPath(thread)
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.close(ctx); err != nil {
		t.Fatal(err)
	}
	full, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	loaded, complete := loadToolLoopPlan(t, root, thread)
	if len(complete.Compactions) != 1 || complete.Compactions[0].Completed == nil || len(complete.Stops) != 2 || requests.Load() != 3 || runner.runs.Load() != 3 {
		t.Fatalf("fixture did not compact during Stop continuation: requests=%d checks=%d", requests.Load(), runner.runs.Load())
	}
	wantFeedback, ok := complete.Stops[0].Decision.Feedback()
	if !ok {
		t.Fatal("first Stop check did not freeze feedback")
	}
	for _, record := range loaded.Records {
		if record.EventKind != session.EventPreCompactHookStarted && record.EventKind != session.EventPreCompactHookDecided {
			continue
		}
		t.Run(string(record.EventKind), func(t *testing.T) {
			writeJournalPrefix(t, journal, full, record.Sequence)
			prefix, err := os.ReadFile(journal)
			if err != nil {
				t.Fatal(err)
			}
			loadFuture := func(context.Context, string, string) (domain.HookSnapshot, error) {
				data, err := os.ReadFile(journal)
				if err != nil {
					return domain.HookSnapshot{}, err
				}
				last := bytes.TrimSuffix(data, []byte{'\n'})
				last = last[bytes.LastIndexByte(last, '\n')+1:]
				terminal, err := session.DecodeRecord(last)
				if err != nil || terminal.EventKind != session.EventTurnFailed {
					t.Fatal("startup Hook configuration loaded before recovery closed the old turn", err)
				}
				return domain.NewHookSnapshot(nil, nil)
			}
			for range 2 {
				resumed, err := openChatResourcesWithHooks(ctx, cfg, paths, string(thread), false, workspace, chatResourceHooks{interactiveApprovals: true, loadHooks: loadFuture, newHookRunner: makeRunner})
				if err != nil {
					t.Fatal(err)
				}
				if err := resumed.close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			recovered, plan := loadToolLoopPlan(t, root, thread)
			contents, err := os.ReadFile(journal)
			if err != nil || !bytes.HasPrefix(contents, prefix) || requests.Load() != 3 || runner.runs.Load() != 3 {
				t.Fatal("resume modified the checked prefix or repeated an effect", err)
			}
			if plan.InterruptedCompaction != nil || plan.StopRecovery != nil || len(plan.Compactions) != 1 || len(plan.Stops) != 1 || len(plan.Turns) != 1 {
				t.Fatal("resume left an open compaction or Stop turn")
			}
			operation := plan.Compactions[0]
			state := plan.Stops[0]
			gotFeedback, ok := state.Decision.Feedback()
			if operation.Failed == nil || operation.Failed.Code != protocol.CompactionFailureInterrupted || operation.TerminalSequence != record.Sequence+1 || state.DeliverySequence <= operation.TerminalSequence || !ok || gotFeedback.Text != wantFeedback.Text {
				t.Fatal("resume did not close the check before delivering exact saved feedback")
			}
			if len(recovered.Records) != int(record.Sequence)+3 || recovered.Records[len(recovered.Records)-1].EventKind != session.EventTurnFailed {
				t.Fatal("resume added a synthetic check result, repeated compensation, or skipped the turn terminal")
			}
		})
	}
}

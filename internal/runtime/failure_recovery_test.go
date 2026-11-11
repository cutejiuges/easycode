package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
)

func loadFailureRecovery(t *testing.T, name string) (session.ToolRecoveryPlan, uint64) {
	t.Helper()
	fixed, err := os.ReadFile(filepath.Join("..", "session", "testdata", "current", "post_hook_"+name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := session.DecodeRecord(bytes.SplitN(fixed, []byte("\n"), 2)[0])
	if err != nil {
		t.Fatal(err)
	}
	repo, err := session.OpenOrCreateRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Create(context.Background(), first.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	path, _ := repo.JournalPath(first.ThreadID)
	if err := os.WriteFile(path, fixed, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := session.NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := session.NewReplayPlanner().Plan(loaded)
	if err != nil || plan.ToolRecovery == nil {
		t.Fatal("missing recovery", err)
	}
	return plan.ToolRecovery.Clone(), loaded.NextSequence
}

func TestFailureRecoveryAllFrozenCutsNeverRunExternalCapabilities(t *testing.T) {
	for _, name := range []string{"failure_exec_start_failed", "failure_result", "failure_started", "failure_prefix", "failure_prefix_started", "failure_completed", "failure_recovered_started", "failure_recovered_unstarted", "failure_cancelled", "failure_unmatched", "failure_route_none", "failure_none", "failure_budget_suffix"} {
		t.Run(name, func(t *testing.T) {
			plan, next := loadFailureRecovery(t, name)
			j := &fakeJournal{sequence: next - 1}
			r, _, runner, preparer, executor := postRuntimeFixture(t, j, domain.HookFeedbackModel)
			if err := r.ReconcileToolTurn(context.Background(), plan); err != nil {
				t.Fatal(err)
			}
			if runner.calls != 0 || preparer.calls != 0 || executor.calls != 0 {
				t.Fatal("recovery invoked external capability")
			}
			conversation := r.conversation.(*fakeToolConversation)
			if conversation.calls.Load() != 0 || len(conversation.results) != 1 {
				t.Fatal("recovery sampled provider or lost output")
			}
			original := plan.Calls()[0]
			before, _ := session.NewToolCallResultDraft(plan.TurnID(), original.Result)
			after, _ := session.NewToolCallResultDraft(plan.TurnID(), conversation.results[0])
			if !bytes.Equal(before.PayloadBytes(), after.PayloadBytes()) {
				t.Fatal("recovery changed original frozen error")
			}
			state := r.hooks.post[original.Invocation.InvocationID()]
			if state.terminal == nil {
				t.Fatal("missing recovery terminal")
			}
			if original.PostHooks.Terminal != nil {
				if state.terminal.Digest != original.PostHooks.Terminal.Digest {
					t.Fatal("accepted terminal was rewritten")
				}
			} else if len(state.terminal.SuffixFeedback) != 0 {
				t.Fatal("recovery generated new feedback")
			}
			for _, batch := range j.snapshot() {
				for _, d := range batch {
					if d.EventKind() == session.EventToolCallResult || d.EventKind() == session.EventHookExecutionStarted {
						t.Fatal("recovery repeated an admission/result")
					}
				}
			}
		})
	}
}
func TestFailureRecoverySyncFaultsPoisonBeforeNewAdmission(t *testing.T) {
	for _, kind := range []session.EventKind{session.EventHookExecutionFinished, session.EventToolPostHookCompleted, session.EventProviderNativeCommit, session.EventTurnFailed} {
		for _, after := range []bool{false, true} {
			t.Run(string(kind)+map[bool]string{false: "/before", true: "/after"}[after], func(t *testing.T) {
				plan, next := loadFailureRecovery(t, "failure_prefix_started")
				j := &hookFaultJournal{fakeJournal: fakeJournal{sequence: next - 1}, kind: kind, after: after, failure: errors.New("injected Sync failure")}
				r, _, runner, preparer, executor := postRuntimeFixture(t, j, domain.HookFeedbackModel)
				if err := r.ReconcileToolTurn(context.Background(), plan); err == nil || !r.poisoned.Load() {
					t.Fatal("unconfirmed recovery did not poison", err)
				}
				if err := r.ReconcileToolTurn(context.Background(), plan); err == nil {
					t.Fatal("poisoned recovery admitted")
				}
				if runner.calls != 0 || preparer.calls != 0 || executor.calls != 0 {
					t.Fatal("failed recovery ran external operation")
				}
			})
		}
	}
}

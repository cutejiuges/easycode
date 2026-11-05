package runtime

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestHookRecoveryMatrixNeverCallsExternalCapabilities(t *testing.T) {
	for _, phase := range []string{"planned", "prefix", "started", "continued", "result", "outputs"} {
		t.Run(phase, func(t *testing.T) {
			journal := &fakeJournal{}
			r, inv, runner, ds := hookRuntimeFixture(t, journal, 2)
			refs := []domain.HookReference{ds[0].Reference, ds[1].Reference}
			p, _ := domain.NewHookPlan(string(inv.InvocationID()), domain.HookPatch, refs)
			g := &session.ReplayedHookGate{Plan: p, PlanSequence: 5}
			call := session.ReplayedToolCall{ReadySequence: 4, Invocation: inv, State: session.ReplayedToolCallReady, Hooks: g}
			if phase == "prefix" || phase == "continued" || phase == "result" {
				id, _ := domain.NewHookRunIdentity(p, 0)
				g.Results = append(g.Results, domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"})
			}
			if phase == "started" {
				id, _ := domain.NewHookRunIdentity(p, 0)
				g.Started = &id
				g.StartedSequence = 6
			}
			if phase == "continued" {
				id, _ := domain.NewHookRunIdentity(p, 1)
				g.Results = append(g.Results, domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"})
				d, _ := domain.NewHookGateDecision(p, domain.HookContinue, domain.HookOK, 2, -1)
				g.Decision = &d
				g.DecisionSequence = 10
			}
			if phase == "result" {
				d, _ := domain.NewHookGateDecision(p, domain.HookBlock, domain.HookTrustDenied, 1, 1)
				g.Decision = &d
				g.DecisionSequence = 9
				result := tool.NewInvocationErrorResult(inv, tool.ResultDenied, string(domain.HookTrustDenied), "frozen safe preview")
				result, _ = tool.FinalizeResult(result, tool.SafetyResultBytes, tool.BudgetRecovery)
				call.State = session.ReplayedToolCallResult
				call.ResultSequence = 10
				call.Result = result
			}
			calls := []session.ReplayedToolCall{call}
			if phase == "outputs" {
				calls = nil
			}
			plan, err := session.NewToolRecoveryPlan(runtimeTurnID, 1, calls, phase == "outputs")
			if err != nil {
				t.Fatal(err)
			}
			if err := r.ReconcileToolTurn(context.Background(), plan); err != nil {
				t.Fatal(err)
			}
			if runner.runs != 0 || runner.preflights != 0 {
				t.Fatal("recovery invoked hook")
			}
			conversation := r.conversation.(*fakeToolConversation)
			if conversation.calls.Load() != 0 {
				t.Fatal("recovery contacted provider")
			}
			batches := journal.snapshot()
			last := batches[len(batches)-1]
			failure := decodeRuntimeFailureDraft(t, last[0])
			wantFailure := "session_interrupted"
			if phase == "started" {
				wantFailure = "outcome_uncertain"
			}
			if failure.Code != wantFailure {
				t.Fatal(failure)
			}
			if phase == "planned" || phase == "prefix" || phase == "started" {
				first := batches[0]
				want := 2
				if phase == "started" {
					want = 3
				}
				if len(first) != want || first[len(first)-1].EventKind() != session.EventToolCallResult {
					t.Fatal("non atomic compensation")
				}
				result := decodeRuntimeResultDraft(t, first[len(first)-1], inv)
				code := string(domain.HookInterrupted)
				if phase == "started" {
					code = string(domain.HookOutcomeUncertain)
				}
				if result.Status() != tool.ResultCancelled || result.Code() != code {
					t.Fatal(result.Status(), result.Code())
				}
			}
			if phase == "result" {
				for _, batch := range batches {
					for _, d := range batch {
						if d.EventKind() == session.EventToolCallResult {
							t.Fatal("rewrote frozen result")
						}
					}
				}
			}
		})
	}
}

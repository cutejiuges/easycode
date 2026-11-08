package session

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

func hookReplayFixture(t *testing.T, n int) (*replayFixture, tool.Invocation, domain.HookPlan) {
	t.Helper()
	f := newReplayFixture(t)
	f.appendMetadata()
	f.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	f.appendInitialInputs(testTurnID)
	inv := sessionPatchInvocation(t)
	ready, err := NewToolCallReadyDraft(testTurnID, inv, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()), mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)), ready)
	refs := make([]domain.HookReference, n)
	for i := range refs {
		d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "check-"+string(rune('a'+i)), 7, "cat >/dev/null", 5)
		if err != nil {
			t.Fatal(err)
		}
		refs[i] = d.Reference
	}
	p, err := domain.NewHookPlan(string(inv.InvocationID()), domain.HookPatch, refs)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewToolHookGatePlannedDraft(testTurnID, p)
	if err != nil {
		t.Fatal(err)
	}
	f.appendBatch(d)
	return f, inv, p
}
func hookStarted(t *testing.T, p domain.HookPlan, n int) (RecordDraft, domain.HookRunIdentity) {
	t.Helper()
	id, err := domain.NewHookRunIdentity(p, n)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewHookExecutionStartedDraft(testTurnID, id)
	if err != nil {
		t.Fatal(err)
	}
	return d, id
}
func hookFinished(t *testing.T, p domain.HookPlan, id domain.HookRunIdentity, status domain.HookOutcome, code domain.HookCode) RecordDraft {
	t.Helper()
	r := domain.HookRunResult{Identity: id, Status: status, Code: code, ExitKnown: true, Origin: "execution", CleanupConfirmed: true}
	if status == domain.HookBlock {
		r.ExitCode = 2
	}
	finished, err := domain.NewEmptyHookFinished(r, p.Hooks[id.Ordinal])
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewHookExecutionFinishedDraft(testTurnID, finished)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func hookDecision(t *testing.T, p domain.HookPlan, status domain.HookOutcome, code domain.HookCode, completed, blocking int) RecordDraft {
	t.Helper()
	v, err := domain.NewHookGateDecision(p, status, code, completed, blocking)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewToolHookGateDecidedDraft(testTurnID, v)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestHookReplayKeepsStartedAndSuccessfulPrefixesDistinct(t *testing.T) {
	for _, phase := range []string{"planned", "started", "prefix", "continued", "denied", "trust-denied"} {
		t.Run(phase, func(t *testing.T) {
			f, inv, p := hookReplayFixture(t, 2)
			if phase != "planned" && phase != "trust-denied" {
				start, id := hookStarted(t, p, 0)
				f.appendBatch(start)
				if phase != "started" {
					finish := hookFinished(t, p, id, domain.HookContinue, domain.HookOK)
					f.appendBatch(finish)
				}
			}
			if phase == "continued" || phase == "denied" {
				start, id := hookStarted(t, p, 1)
				f.appendBatch(start)
				if phase == "continued" {
					f.appendBatch(hookFinished(t, p, id, domain.HookContinue, domain.HookOK), hookDecision(t, p, domain.HookContinue, domain.HookOK, 2, -1))
				} else {
					result := tool.NewInvocationErrorResult(inv, tool.ResultDenied, string(domain.HookDenied), "Hook stopped")
					d, err := NewToolCallResultDraft(testTurnID, result)
					if err != nil {
						t.Fatal(err)
					}
					f.appendBatch(hookFinished(t, p, id, domain.HookBlock, domain.HookDenied), hookDecision(t, p, domain.HookBlock, domain.HookDenied, 2, 1), d)
				}
			}
			if phase == "trust-denied" {
				result := tool.NewInvocationErrorResult(inv, tool.ResultDenied, string(domain.HookTrustRequired), "Hook trust required")
				d, err := NewToolCallResultDraft(testTurnID, result)
				if err != nil {
					t.Fatal(err)
				}
				f.appendBatch(hookDecision(t, p, domain.HookBlock, domain.HookTrustRequired, 0, 0), d)
			}
			plan, err := NewReplayPlanner().Plan(f.loaded())
			if err != nil {
				t.Fatal(err)
			}
			if plan.ToolRecovery == nil {
				t.Fatal("missing recovery plan")
			}
			calls := plan.ToolRecovery.Calls()
			g := calls[0].Hooks
			if g == nil || g.Plan.Digest != p.Digest {
				t.Fatal("lost hook plan")
			}
			if phase == "started" && (g.Started == nil || calls[0].StartedSequence != 0) {
				t.Fatal("hook start became tool start")
			}
			if phase == "prefix" && (len(g.Results) != 1 || g.Decision != nil) {
				t.Fatal("lost prefix")
			}
			if phase == "continued" && (g.Decision == nil || g.Decision.Status != domain.HookContinue) {
				t.Fatal("gate not continued")
			}
			if (phase == "denied" || phase == "trust-denied") && calls[0].Result.Status() != tool.ResultDenied {
				t.Fatal("denied result not paired")
			}
			g.Plan.Hooks[0].ID = "changed"
			if plan.ToolRecovery.Calls()[0].Hooks.Plan.Hooks[0].ID == "changed" {
				t.Fatal("recovery plan aliases returned value")
			}
		})
	}
}

func TestHookReplayRejectsSkippedDuplicatedAndNonAtomicFacts(t *testing.T) {
	for _, bad := range []string{"duplicate-plan", "skipped-start", "missing-finish", "split-denial", "finish-without-start", "approval-before-gate", "result-before-gate", "wrong-result"} {
		t.Run(bad, func(t *testing.T) {
			f, inv, p := hookReplayFixture(t, 2)
			switch bad {
			case "duplicate-plan":
				d, _ := NewToolHookGatePlannedDraft(testTurnID, p)
				f.appendBatch(d)
			case "skipped-start":
				d, _ := hookStarted(t, p, 1)
				f.appendBatch(d)
			case "missing-finish":
				f.appendBatch(hookDecision(t, p, domain.HookContinue, domain.HookOK, 2, -1))
			case "finish-without-start":
				_, id := hookStarted(t, p, 0)
				f.appendBatch(hookFinished(t, p, id, domain.HookContinue, domain.HookOK))
			case "split-denial":
				f.appendBatch(hookDecision(t, p, domain.HookBlock, domain.HookTrustDenied, 0, 0))
			case "approval-before-gate":
				metadata, _ := NewApprovalMetadata(inv)
				id, _ := tool.ParseApprovalRequestID(string(inv.InvocationID()))
				d, err := NewToolApprovalDecidedDraft(testTurnID, ToolApprovalDecidedPayload{RequestID: id, InvocationID: inv.InvocationID(), Fingerprint: tool.NewCandidateFingerprint([]byte("candidate")), Outcome: tool.ApprovalOutcomeAllowOnce, Source: ApprovalSourceInteractive, Metadata: metadata})
				if err != nil {
					t.Fatal(err)
				}
				f.appendBatch(d)
			case "result-before-gate":
				d, _ := NewToolCallResultDraft(testTurnID, tool.NewInvocationErrorResult(inv, tool.ResultCancelled, "cancelled", "cancelled"))
				f.appendBatch(d)
			case "wrong-result":
				d, _ := NewToolCallResultDraft(testTurnID, tool.NewInvocationErrorResult(inv, tool.ResultCancelled, "cancelled", "cancelled"))
				f.appendBatch(hookDecision(t, p, domain.HookBlock, domain.HookTrustDenied, 0, 0), d)
			}
			if _, err := NewReplayPlanner().Plan(f.loaded()); err == nil {
				t.Fatal("illegal hook lifecycle accepted")
			}
		})
	}
}

func TestHookRecordCodecsRejectRawOutputAndUnknownFields(t *testing.T) {
	_, _, p := hookReplayFixture(t, 1)
	start, id := hookStarted(t, p, 0)
	finish := hookFinished(t, p, id, domain.HookContinue, domain.HookOK)
	decision := hookDecision(t, p, domain.HookContinue, domain.HookOK, 1, -1)
	planned, _ := NewToolHookGatePlannedDraft(testTurnID, p)
	for _, d := range []RecordDraft{planned, start, finish, decision} {
		for _, field := range []string{"stdout", "schema_version", "command", "pid"} {
			b := bytes.TrimSuffix(d.PayloadBytes(), []byte("}"))
			b = append(b, []byte(`,"`+field+`":"private"}`)...)
			if err := decodeKnownRecord(Record{EventKind: d.EventKind(), TurnID: testTurnID, Payload: b}); err == nil {
				t.Fatalf("accepted %s in %s", field, d.EventKind())
			}
		}
		if err := codec.ValidateJSON(d.PayloadBytes()); err != nil {
			t.Fatal(err)
		}
	}
}

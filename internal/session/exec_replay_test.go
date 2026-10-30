package session

import (
	"testing"

	"easycode/internal/tool"
)

func replayExecInvocation(t *testing.T) tool.Invocation {
	t.Helper()
	input, _ := tool.NewExecInput("printf hello", "src", 1000)
	ready, _ := tool.NewExecReadyCall("call-exec", input)
	id, _ := tool.ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789ad")
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func replayExecResult(t *testing.T, invocation tool.Invocation, status tool.ResultStatus, code string) RecordDraft {
	t.Helper()
	exec, _ := invocation.Exec()
	result := tool.NewExecErrorResult(exec, status, code, "frozen Exec output")
	if status == tool.ResultSuccess {
		metadata, _ := tool.NewExecResultMetadata("src", 2, true, 123, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
		preview, _ := tool.NewModelPreview("frozen Exec output")
		var err error
		result, err = tool.NewExecInvocationResult(exec, status, code, preview, metadata)
		if err != nil {
			t.Fatal(err)
		}
	}
	draft, err := NewToolCallResultDraft(testTurnID, result)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func TestReplayExecCrashBoundariesAndInvalidTransitions(t *testing.T) {
	for _, test := range []struct {
		name            string
		approval        tool.ApprovalOutcome
		started, result bool
		status          tool.ResultStatus
		code            string
		invalid         bool
	}{
		{name: "ready"},
		{name: "allowed", approval: tool.ApprovalOutcomeAllowOnce},
		{name: "started", approval: tool.ApprovalOutcomeAllowOnce, started: true},
		{name: "result awaiting native output", approval: tool.ApprovalOutcomeAllowOnce, started: true, result: true, status: tool.ResultSuccess, code: "ok"},
		{name: "denied", approval: tool.ApprovalOutcomeDeny, result: true, status: tool.ResultDenied, code: "approval_denied"},
		{name: "cancelled before admission", approval: tool.ApprovalOutcomeAllowOnce, result: true, status: tool.ResultCancelled, code: "cancelled_before_execution"},
		{name: "recovered before admission", approval: tool.ApprovalOutcomeAllowOnce, result: true, status: tool.ResultCancelled, code: "session_interrupted_before_execution"},
		{name: "started without approval", started: true, invalid: true},
		{name: "started after deny", approval: tool.ApprovalOutcomeDeny, started: true, invalid: true},
		{name: "result without start", approval: tool.ApprovalOutcomeAllowOnce, result: true, status: tool.ResultSuccess, code: "ok", invalid: true},
		{name: "session grant", approval: tool.ApprovalOutcomeAllowSession, invalid: true},
		{name: "reused session grant", approval: tool.ApprovalOutcomeAllowFromSession, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReplayFixture(t)
			fixture.appendMetadata()
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendInitialInputs(testTurnID)
			invocation := replayExecInvocation(t)
			appendReplayCallSample(t, fixture, invocation)
			fingerprint := tool.NewCandidateFingerprint([]byte("exec-candidate"))
			deniedPair := test.approval == tool.ApprovalOutcomeDeny && test.result && !test.started
			if deniedPair {
				fixture.appendBatch(mustPatchApprovalDraft(t, invocation, fingerprint, test.approval), replayExecResult(t, invocation, test.status, test.code))
			} else if test.approval != "" {
				fixture.appendBatch(mustPatchApprovalDraft(t, invocation, fingerprint, test.approval))
			}
			if test.started {
				fixture.appendBatch(mustStartedDraftForReplay(t, invocation))
			}
			if test.result && !deniedPair {
				fixture.appendBatch(replayExecResult(t, invocation, test.status, test.code))
			}
			plan, err := NewReplayPlanner().Plan(fixture.loaded())
			if test.invalid {
				if err == nil {
					t.Fatal("invalid Exec replay accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Tools) != 1 || plan.Tools[0].Invocation.InvocationID() != invocation.InvocationID() || plan.Tools[0].HasResult != test.result {
				t.Fatal("command history lost interrupted or terminal facts")
			}
			if test.result && (plan.Tools[0].Result.Status() != test.status || plan.Tools[0].Result.Preview().Text() != "frozen Exec output") {
				t.Fatal("command history changed frozen terminal state")
			}
			calls := plan.ToolRecovery.Calls()
			want := ReplayedToolCallReady
			if test.started {
				want = ReplayedToolCallStarted
			}
			if test.result {
				want = ReplayedToolCallResult
			}
			if len(calls) != 1 || calls[0].State != want || calls[0].ApprovalOutcome != test.approval || calls[0].Invocation.Capability() != tool.CapabilityExec {
				t.Fatal("Exec crash boundary changed")
			}
			if test.approval != "" && calls[0].CandidateFingerprint != fingerprint {
				t.Fatal("Exec candidate identity lost")
			}
			if test.result && calls[0].Result.Preview().Text() != "frozen Exec output" {
				t.Fatal("Exec replay changed frozen preview")
			}
		})
	}
}

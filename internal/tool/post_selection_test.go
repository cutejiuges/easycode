package tool

import (
	"easycode/internal/domain"
	"strings"
	"testing"
)

func selectionExec(t *testing.T, p domain.PostHookPlan, status ResultStatus, code string, exit int) InvocationResult {
	t.Helper()
	input, _ := NewExecInput("go test ./...", "", 1000)
	ready, _ := NewExecReadyCall("call", input)
	inv, _ := NewExecInvocation(InvocationID(p.InvocationID), ready)
	result := NewExecErrorResult(inv, status, code, "PRIVATE-ERROR-SENTINEL")
	if status == ResultSuccess {
		metadata, _ := NewExecResultMetadata("", exit, true, 1, ExecStreamStats{}, ExecStreamStats{}, false, false, false)
		preview, _ := NewModelPreview("original output")
		var err error
		result, err = NewExecInvocationResult(inv, status, code, preview, metadata)
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := FinalizeResult(result, p.Allowance-p.Reservation, BudgetPreserved)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestPostRouteUsesOnlyDurableAttemptAndOriginalResult(t *testing.T) {
	p := feedbackPlan(t, domain.HookFeedbackModel)
	admission, _ := NewExecutionAdmission(InvocationID(p.InvocationID), 17)
	for _, tc := range []struct {
		status ResultStatus
		code   string
		exit   int
		route  domain.PostHookRoute
	}{
		{ResultSuccess, "ok", 0, domain.PostHookRouteSuccess},
		{ResultSuccess, "ok", 1, domain.PostHookRouteSuccess},
		{ResultError, "process_start_failed", 0, domain.PostHookRouteFailure},
		{ResultError, "tool_timed_out", 0, domain.PostHookRouteFailure},
		{ResultDenied, "permission_denied", 0, domain.PostHookRouteNone},
		{ResultCancelled, "cancelled", 0, domain.PostHookRouteNone},
		{ResultOutcomeUncertain, "outcome_uncertain", 0, domain.PostHookRouteNone},
	} {
		t.Run(tc.code+string(tc.status), func(t *testing.T) {
			r := selectionExec(t, p, tc.status, tc.code, tc.exit)
			route, err := SelectPostHookRoute(r, admission)
			if err != nil || route != tc.route {
				t.Fatal("wrong original fact selection", route, err)
			}
			if route, err := SelectPostHookRoute(r, ExecutionAdmission{}); err != nil || route != domain.PostHookRouteNone {
				t.Fatal("missing admission selected event", route, err)
			}
			other, _ := NewExecutionAdmission("018f1d8a-7b5c-7def-8abc-456789abcdea", 18)
			if _, err := SelectPostHookRoute(r, other); err == nil {
				t.Fatal("cross-call admission accepted")
			}
		})
	}
	if _, err := NewExecutionAdmission(InvocationID(p.InvocationID), 0); err == nil {
		t.Fatal("zero durable sequence accepted")
	}
}
func TestPostOutputRejectsOtherBranchEvenAtSameOrdinal(t *testing.T) {
	p := feedbackPlan(t, domain.HookFeedbackModel)
	d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUseFailure, domain.HookFeedbackModel, strings.Repeat("a", 64), "failure", 7, "true", 5)
	b, _ := domain.NewPostHookBranch(domain.HookPostToolUseFailure, []domain.HookReference{d.Reference}, p.Reservation)
	p, err := domain.NewPostHookPlan(p.InvocationID, p.Capability, append(p.Branches, b), p.Allowance, p.Reservation)
	if err != nil {
		t.Fatal(err)
	}
	r := selectionExec(t, p, ResultError, "process_start_failed", 0)
	admission, _ := NewExecutionAdmission(r.InvocationID(), 19)
	selection, _ := NewPostHookSelection(p, admission, r)
	candidate, _ := domain.NewHookFeedbackCandidate("authorized failure feedback")
	freeze := func(event domain.HookEvent) domain.FrozenHookFeedback {
		id, _ := domain.NewPostHookRunIdentity(p, event, 0)
		stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
		f, err := FreezePostFeedback(p, nil, stats, candidate)
		if err != nil {
			t.Fatal(err)
		}
		return f.Feedback
	}
	if _, err := NewPostModelOutput(r, selection, []domain.FrozenHookFeedback{freeze(domain.HookPostToolUse)}); err == nil {
		t.Fatal("other branch ordinal zero accepted")
	}
	output, err := NewPostModelOutput(r, selection, []domain.FrozenHookFeedback{freeze(domain.HookPostToolUseFailure)})
	if err != nil || output.Validate() != nil || output.Result().Status() != ResultError || !strings.Contains(output.Text(), "authorized failure feedback") {
		t.Fatal("valid failure feedback lost", err)
	}
	withoutAdmission, _ := NewPostHookSelection(p, ExecutionAdmission{}, r)
	if _, err := NewPostModelOutput(r, withoutAdmission, output.Feedback()); err == nil {
		t.Fatal("feedback without attempt admitted")
	}
}

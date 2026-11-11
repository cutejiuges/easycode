package tool

import (
	"easycode/internal/domain"
	"strings"
	"testing"
)

func feedbackPlan(t *testing.T, policies ...domain.HookFeedbackPolicy) domain.PostHookPlan {
	t.Helper()
	refs := make([]domain.HookReference, len(policies))
	for i, p := range policies {
		d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, p, strings.Repeat("a", 64), strings.Repeat(string(rune('a'+i)), 64), 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		refs[i] = d.Reference
	}
	p, err := domain.NewHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", domain.HookExec, refs)
	if err != nil {
		t.Fatal(err)
	}
	currentPostBranch1, _ := domain.NewPostHookBranch(p.Event, p.Hooks, 16<<10)
	currentPostBranch1.Delivery = domain.HookDeliveryAvailable
	post, err := domain.NewPostHookPlan(p.InvocationID, p.Capability, []domain.PostHookBranch{currentPostBranch1}, 256<<10, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	return post
}
func postResult(t *testing.T, p domain.PostHookPlan, i int, status domain.HookOutcome, code domain.HookCode) domain.HookRunResult {
	t.Helper()
	id, err := domain.NewPostHookRunIdentity(p, domain.HookPostToolUse, i)
	if err != nil {
		t.Fatal(err)
	}
	exit := 0
	if code == domain.HookExitFailed {
		exit = 2
	}
	r, err := domain.NewHookRunResult(domain.HookRunResult{Identity: id, Status: status, Code: code, ExitKnown: true, ExitCode: exit, CleanupConfirmed: true, Origin: "execution"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestPostFeedbackRetainsDurablePrefixAndNoticeSlots(t *testing.T) {
	p := feedbackPlan(t, domain.HookFeedbackModel, domain.HookFeedbackNone, domain.HookFeedbackModel)
	candidate, _ := domain.NewHookFeedbackCandidate(strings.Repeat("a", 4096))
	first, err := FreezePostFeedback(p, nil, postResult(t, p, 0, domain.HookContinue, domain.HookOK), candidate)
	if err != nil || first.Feedback.Kind != domain.HookFeedbackContent {
		t.Fatal(err)
	}
	prefix := []domain.FrozenHookFeedback{first.Feedback}
	middle, err := FreezePostFeedback(p, prefix, postResult(t, p, 1, domain.HookContinue, domain.HookOK), domain.HookFeedbackCandidate{})
	if err != nil || middle.Feedback.Kind != domain.HookFeedbackEmpty {
		t.Fatal(err)
	}
	prefix = append(prefix, middle.Feedback)
	last, err := FreezePostFeedback(p, prefix, postResult(t, p, 2, domain.HookError, domain.HookExitFailed), domain.HookFeedbackCandidate{})
	if err != nil || last.Feedback.Kind != domain.HookFeedbackCode {
		t.Fatal(err)
	}
	prefix = append(prefix, last.Feedback)
	text, err := RenderPostFeedback(prefix)
	if err != nil || !strings.Contains(text, candidate.Content()) || !strings.Contains(text, "hook_exit_failed") || prefix[0] != first.Feedback {
		t.Fatal("prefix lost", err)
	}
	total := 0
	for _, item := range prefix {
		total += item.BudgetDelta
	}
	if total != len(text) {
		t.Fatal("wrong delta", total, len(text))
	}
}
func TestPostFeedbackEscapingAndBudgetUnavailable(t *testing.T) {
	p := feedbackPlan(t, domain.HookFeedbackModel)
	r := postResult(t, p, 0, domain.HookContinue, domain.HookOK)
	candidate, _ := domain.NewHookFeedbackCandidate(strings.Repeat("<", 4096))
	f, err := FreezePostFeedback(p, nil, r, candidate)
	if err != nil || f.Result.Code != domain.HookFeedbackBudgetExceeded || f.Feedback.Kind != domain.HookFeedbackCode || f.Feedback.Content != "" {
		t.Fatal("escaped body not rejected whole", err)
	}
	smallBranch, _ := domain.NewPostHookBranch(domain.HookPostToolUse, p.Branches[0].Hooks, 500)
	unavailable, err := domain.NewPostHookPlan(p.InvocationID, p.Capability, []domain.PostHookBranch{smallBranch}, 1524, 500)
	if err != nil {
		t.Fatal(err)
	}
	unavailableResult := postResult(t, unavailable, 0, domain.HookContinue, domain.HookOK)
	f, err = FreezePostFeedback(unavailable, nil, unavailableResult, candidate)
	if err != nil || f.Feedback.Kind != domain.HookFeedbackEmpty || f.Result.Code != domain.HookOK {
		t.Fatal("unavailable reservation disclosed", err)
	}
	text, err := RenderPostFeedback([]domain.FrozenHookFeedback{f.Feedback})
	if err != nil || text != "" {
		t.Fatal("empty disclosure changed output", err)
	}
}
func TestPostNoticeWorstCaseFitsReservedSlot(t *testing.T) {
	p := feedbackPlan(t, domain.HookFeedbackModel)
	for _, code := range []domain.HookCode{domain.HookFeedbackBudgetExceeded, domain.HookExecutionBudgetExhausted, domain.HookPostInterrupted, domain.HookSandboxUnavailable} {
		id, _ := domain.NewPostHookRunIdentity(p, domain.HookPostToolUse, 0)
		f, err := domain.NewFrozenHookFeedback(id, p.Branches[0].Hooks[0], domain.HookFeedbackCode, "", code, 1)
		if err != nil {
			t.Fatal(err)
		}
		text, err := RenderPostFeedback([]domain.FrozenHookFeedback{f})
		if err != nil || len(text) > PostFeedbackNoticeBytes {
			t.Fatal("notice exceeds slot", len(text), err)
		}
	}
}
func TestResultTicketDurableSegmentsAndSingleRetirement(t *testing.T) {
	b := NewResultBudget()
	if err := b.BeginSample(2); err != nil {
		t.Fatal(err)
	}
	ticket, err := b.Reserve(CapabilityExec, true)
	if err != nil || ticket.Allowance() != 256<<10 || ticket.Reservation() != 16<<10 || ticket.BaseAllowance() != 240<<10 {
		t.Fatal("reservation", err)
	}
	if b.TurnUsed() != 0 || b.SampleUsed() != 0 {
		t.Fatal("reservation charged")
	}
	if _, err := b.Reserve(CapabilityRead, false); err == nil {
		t.Fatal("crossed active ticket")
	}
	if _, err := b.Allowance(CapabilityRead); err == nil {
		t.Fatal("admitted follower")
	}
	m, _ := NewResultBudgetMetadata(ticket.BaseAllowance(), 2000, BudgetPreserved)
	if err := ticket.CommitBase(m); err != nil {
		t.Fatal(err)
	}
	if err := ticket.CommitBase(m); err == nil {
		t.Fatal("duplicate base")
	}
	if err := ticket.CommitFeedback(1, 500); err != nil {
		t.Fatal(err)
	}
	if err := ticket.CommitFeedback(1, 500); err == nil {
		t.Fatal("duplicate durable feedback fact")
	}
	if b.TurnUsed() != 2500 || b.processed != 0 || b.sampleRemaining != 2 {
		t.Fatal("premature retirement")
	}
	if err := ticket.CommitFeedback(2, 16<<10); err == nil {
		t.Fatal("over budget")
	}
	if err := ticket.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ticket.Close(); err == nil {
		t.Fatal("duplicate close")
	}
	if b.processed != 1 || b.sampleRemaining != 1 {
		t.Fatal("wrong retirement")
	}
	if _, err := b.Reserve(CapabilityRead, false); err != nil {
		t.Fatal(err)
	}
}

func TestModelEmptyFeedbackDoesNotExpandReservedExecPreview(t *testing.T) {
	plan := feedbackPlan(t, domain.HookFeedbackModel)
	budget := NewResultBudget()
	if err := budget.BeginSample(1); err != nil {
		t.Fatal(err)
	}
	ticket, err := budget.Reserve(CapabilityExec, true)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr ExecOutputBuffer
	_, _ = stdout.Write([]byte(strings.Repeat("x", 250<<10)))
	meta, _ := NewExecResultMetadata("", 7, true, 1, stdout.Snapshot().Stats(), stderr.Snapshot().Stats(), false, false, false)
	preview, cut, err := RenderExecOutput(stdout.Snapshot(), stderr.Snapshot(), meta, ticket.BaseAllowance())
	if err != nil || !cut {
		t.Fatal("expected reserved preview shortening", err)
	}
	input, _ := NewExecInput("printf large", "", 1000)
	ready, _ := NewExecReadyCall("call", input)
	inv, _ := NewExecInvocation(InvocationID(plan.InvocationID), ready)
	result, err := NewExecInvocationResult(inv, ResultSuccess, "ok", preview, meta)
	if err != nil {
		t.Fatal(err)
	}
	result, err = FinalizeResult(result, ticket.BaseAllowance(), BudgetPreserved)
	if err != nil {
		t.Fatal(err)
	}
	if result.Budget().Allowance() != 240<<10 || ticket.Reservation() != 16<<10 {
		t.Fatal("wrong A/R/base allowance")
	}
	if err := ticket.CommitBase(result.Budget()); err != nil {
		t.Fatal(err)
	}
	frozen, err := FreezePostFeedback(plan, nil, postResult(t, plan, 0, domain.HookContinue, domain.HookOK), domain.HookFeedbackCandidate{})
	if err != nil {
		t.Fatal(err)
	}
	admission, _ := NewExecutionAdmission(result.InvocationID(), 1)
	selection, err := NewPostHookSelection(plan, admission, result)
	if err != nil {
		t.Fatal(err)
	}
	output, err := NewPostModelOutput(result, selection, []domain.FrozenHookFeedback{frozen.Feedback})
	if err != nil || output.Text() != preview.Text() || result.Status() != ResultSuccess || result.Code() != "ok" {
		t.Fatal("empty feedback expanded/reclassified result", err)
	}
	expanded, _, err := RenderExecOutput(stdout.Snapshot(), stderr.Snapshot(), meta, ticket.Allowance())
	if err != nil || len(expanded.Text()) <= len(output.Text()) {
		t.Fatal("fixture did not distinguish original A and frozen A-R", err)
	}
	if err := ticket.Close(); err != nil {
		t.Fatal(err)
	}
	if budget.SampleUsed() != len(output.Text()) {
		t.Fatal("empty feedback charged unused reservation")
	}
}
func TestResultTicketZeroFeedbackReservationRemainsClosedToBodies(t *testing.T) {
	b := NewResultBudget()
	for !b.BodyExhausted() {
		if err := b.BeginSample(1); err != nil {
			t.Fatal(err)
		}
		allowance, _ := b.Allowance(CapabilityRead)
		metadata, _ := NewResultBudgetMetadata(allowance, allowance, BudgetPreserved)
		if err := b.Commit(CapabilityRead, metadata); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.BeginSample(1); err != nil {
		t.Fatal(err)
	}
	ticket, err := b.Reserve(CapabilityExec, true)
	if err != nil || ticket.Reservation() != 0 || ticket.BaseAllowance() != SafetyResultBytes {
		t.Fatal("zero reservation boundary", err)
	}
	if PostFeedbackAvailable(feedbackPlan(t, domain.HookFeedbackModel).Branches[0], ticket.Reservation()) {
		t.Fatal("unavailable notice space accepted bodies")
	}
	metadata, _ := NewResultBudgetMetadata(ticket.BaseAllowance(), 30, BudgetRejected)
	if err := ticket.CommitBase(metadata); err != nil {
		t.Fatal(err)
	}
	if err := ticket.CommitFeedback(1, 1); err == nil {
		t.Fatal("zero reservation charged feedback")
	}
	if err := ticket.Close(); err != nil {
		t.Fatal(err)
	}
	if !b.BodyExhausted() {
		t.Fatal("unused reservation reopened admission")
	}
}

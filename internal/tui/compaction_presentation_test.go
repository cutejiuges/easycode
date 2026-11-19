package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestPreCompactPresentationIgnoresStaleReviewAndStageEvents(t *testing.T) {
	session := domain.SessionID("01900000-0000-7000-8000-000000000021")
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000022")
	id := domain.CompactionID("01900000-0000-7000-8000-000000000023")
	scope := domain.ManualCompactionScope()
	target, _ := domain.NewPreCompactHookTarget(id, scope)
	matcher, _ := domain.ParsePreCompactHookScope("manual")
	declaration, err := domain.NewPreCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, strings.Repeat("a", 64), "verify", matcher, "printf '{}'", 5)
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Repeat("b", 64)
	review := protocol.HookTrustRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000024", Target: target, PlanDigest: plan, Declaration: declaration}
	requested, err := protocol.NewHookTrustRequested(review)
	if err != nil {
		t.Fatal(err)
	}
	requested.SessionID, requested.ThreadID = session, thread
	model := NewModel(context.Background(), "test", &fakeHookSession{})
	model.prepareCompaction(id, scope)
	if err := model.composer.replace(0, 0, "draft to keep"); err != nil {
		t.Fatal(err)
	}
	thatUpdate := func(event protocol.Event) {
		t.Helper()
		item, err := protocol.NewCorrelatedContextItem("compact-1", event)
		if err != nil {
			t.Fatal(err)
		}
		model = updateModel(t, model, item)
	}
	thatUpdate(requested)
	if model.hookTrust != nil {
		t.Fatal("review opened before the durable start")
	}
	started, _ := protocol.NewCompactionStarted(session, thread, id, 1, scope)
	thatUpdate(started)
	thatUpdate(requested)
	if model.hookTrust == nil || model.hookTrust.request != review {
		t.Fatal("current review did not open")
	}
	response, _ := protocol.NewHookTrustResponse(session, thread, "", review, protocol.HookTrustSession)
	resolved, _ := protocol.NewHookTrustResolved(response)
	thatUpdate(resolved)
	thatUpdate(requested)
	if model.hookTrust != nil {
		t.Fatal("duplicate review reopened the modal")
	}
	checking, err := protocol.NewHookActivity(protocol.HookActivityPayload{Target: target, PlanDigest: plan, Reference: declaration.Reference, State: protocol.HookActivityChecking, Code: domain.HookOK})
	if err != nil {
		t.Fatal(err)
	}
	checking.SessionID, checking.ThreadID = session, thread
	thatUpdate(checking)
	if model.compaction.stage != compactionChecking {
		t.Fatal("checking phase was not active")
	}
	summarizing, _ := protocol.NewCompactionSummarizing(session, thread, id, scope)
	thatUpdate(summarizing)
	thatUpdate(checking)
	thatUpdate(requested)
	if model.compaction.stage != compactionSummarizing || model.hookTrust != nil {
		t.Fatal("late check regressed summary status")
	}
	failed, _ := protocol.NewCompactionFailed(session, thread, id, protocol.CompactionFailedPayload{Scope: scope, Code: protocol.CompactionFailureCancelled, Durable: true, UsageState: "unavailable"})
	thatUpdate(failed)
	model = updateModel(t, model, hookTrustFailedMsg{request: review.RequestID, target: target, err: errors.New("old callback")})
	model = updateModel(t, model, compactionCancelFailedMsg{id: id, summary: "old cancellation"})
	thatUpdate(resolved)
	if model.activeCompactionID != "" || model.hookTrust != nil || model.composer.text != "draft to keep" || model.errorSummary != "" {
		t.Fatal("stale callback changed the next input state")
	}
}

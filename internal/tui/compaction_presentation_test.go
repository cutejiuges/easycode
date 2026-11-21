package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
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
	waitingReview := model
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

	// 本地取消先显示，另一端已经通过审查并提交摘要。
	model = updateModel(t, waitingReview, tea.KeyMsg{Type: tea.KeyCtrlC})
	thatUpdate(resolved)
	thatUpdate(summarizing)
	post := protocol.PostCompactInstalledPayload{Scope: scope, CheckpointSequence: 8, Revision: 2, PlanDigest: strings.Repeat("c", 64), Count: 1}
	installed, err := protocol.NewPostCompactInstalled(session, thread, id, post)
	if err != nil {
		t.Fatal(err)
	}
	thatUpdate(installed)
	if model.hookTrust != nil || model.compaction.post != post || !strings.Contains(model.compactionStatus, "Conversation compacted · cancelling PostCompact") {
		t.Fatal("cancelled pre review hid the successful post installation")
	}
}

func TestPostCompactPresentationKeepsInstalledWindowDuringCancellation(t *testing.T) {
	session := domain.SessionID("01900000-0000-7000-8000-000000000021")
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000022")
	id := domain.CompactionID("01900000-0000-7000-8000-000000000023")
	turn := domain.TurnID("01900000-0000-7000-8000-000000000024")
	scope, err := domain.NewAutoCompactionScope(turn, 1)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := domain.NewCompactionHookTarget(id, scope)
	matcher, _ := domain.ParseCompactionHookScope("*")
	plan := strings.Repeat("b", 64)
	post, err := domain.NewPostCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, strings.Repeat("a", 64), "verify", matcher, "verify-saved-summary", 5)
	if err != nil {
		t.Fatal(err)
	}
	pre, err := domain.NewPreCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, strings.Repeat("a", 64), "pre", matcher, "printf '{}'", 5)
	if err != nil {
		t.Fatal(err)
	}
	host := &fakeHookSession{}
	model := NewModel(context.Background(), "test", host)
	model.activeTurnID = turn
	model.refreshState()
	model.width, model.height = 100, 24
	if err := model.composer.replace(0, 0, "draft to keep"); err != nil {
		t.Fatal(err)
	}
	thatUpdate := func(event protocol.Event, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		event.SessionID, event.ThreadID = session, thread
		item, err := protocol.NewCorrelatedContextItem("turn-1", event)
		if err != nil {
			t.Fatal(err)
		}
		model = updateModel(t, model, item)
	}
	postReview := protocol.HookTrustRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000025", Target: target, PlanDigest: plan, Declaration: post}
	thatUpdate(protocol.NewCompactionStarted(session, thread, id, 1, scope))
	thatUpdate(protocol.NewHookTrustRequested(postReview))
	if model.hookTrust != nil {
		t.Fatal("post review opened before the checkpoint was installed")
	}
	thatUpdate(protocol.NewCompactionSummarizing(session, thread, id, scope))
	installed := protocol.PostCompactInstalledPayload{Scope: scope, CheckpointSequence: 8, Revision: 2, PlanDigest: plan, Count: 1}
	thatUpdate(protocol.NewPostCompactInstalled(session, thread, id, installed))
	preReview := protocol.HookTrustRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000026", Target: target, PlanDigest: plan, Declaration: pre}
	thatUpdate(protocol.NewHookTrustRequested(preReview))
	wrongPlan := postReview
	wrongPlan.PlanDigest = strings.Repeat("c", 64)
	thatUpdate(protocol.NewHookTrustRequested(wrongPlan))
	if model.hookTrust != nil {
		t.Fatal("wrong event or plan reached the current trust modal")
	}
	thatUpdate(protocol.NewHookTrustRequested(postReview))
	if model.hookTrust == nil || model.hookTrust.trust {
		t.Fatal("post review was missing or defaulted to trust")
	}
	review := model.renderHookTrust()
	if !strings.Contains(review, "reads the current compaction summary on stdin") || !strings.Contains(review, post.Command) {
		t.Fatalf("post review omitted disclosure or command: %s", review)
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 19, Height: 5})
	model, command := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || !model.hookReviewUnavailable() {
		t.Fatal("narrow terminal allowed an incomplete review")
	}
	model, command = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if command == nil || !strings.Contains(model.compactionStatus, "Conversation compacted · cancelling PostCompact") {
		t.Fatalf("cancellation lost the status of the saved window: %s", model.compactionStatus)
	}
	command()
	if host.interrupts != 1 {
		t.Fatal("cancellation was not routed to the automatic turn")
	}
	resolved, _ := protocol.NewHookTrustResponse(session, thread, turn, postReview, protocol.HookTrustSession)
	thatUpdate(protocol.NewHookTrustResolved(resolved))
	if model.hookTrust == nil {
		t.Fatal("late trust response retired an active cleanup")
	}
	before, _ := domain.NewEstimatedTokenEstimate("test", 20)
	after, _ := domain.NewEstimatedTokenEstimate("test", 10)
	metric := domain.UnknownUsageMetric()
	usage, _ := domain.NewSampleUsage(metric, metric, metric, metric, metric)
	thatUpdate(protocol.NewCompactionCompleted(session, thread, id, 2, before, after, usage, scope))
	if model.activeCompactionID != id {
		t.Fatal("premature completion retired the cleanup owner")
	}
	result := protocol.PostCompactClosedPayload{Scope: scope, CheckpointSequence: 8, PlanDigest: plan, CloseSequence: 9, State: protocol.PostCompactCancelled, Code: domain.HookCancelled}
	thatUpdate(protocol.NewPostCompactClosed(session, thread, id, result))
	thatUpdate(protocol.NewCompactionCompleted(session, thread, id, 2, before, after, usage, scope))
	if model.activeCompactionID != "" || model.hookTrust != nil || model.composer.text != "draft to keep" ||
		!strings.Contains(model.compactionStatus, "Conversation compacted") || !strings.Contains(model.compactionStatus, "PostCompact cancelled") {
		t.Fatalf("final status did not preserve the saved compaction and its cancelled check: %s", model.compactionStatus)
	}
}

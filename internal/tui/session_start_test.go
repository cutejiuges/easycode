package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCompactSessionStartReviewWaitsForPostAndKeepsKnownSuccess(t *testing.T) {
	const session = domain.SessionID("01900000-0000-7000-8000-000000000021")
	const thread = domain.ThreadID("01900000-0000-7000-8000-000000000022")
	const id = domain.CompactionID("01900000-0000-7000-8000-000000000023")
	const turn = domain.TurnID("01900000-0000-7000-8000-000000000024")
	scope, err := domain.NewAutoCompactionScope(turn, 1)
	if err != nil {
		t.Fatal(err)
	}
	anchor := domain.SessionStartTarget{Source: domain.SessionStartCompact, CompactionID: id, Scope: scope,
		CheckpointSequence: 8, CheckpointRevision: 2, CheckpointDigest: domain.HookDigest([]byte("summary"))}
	target, err := domain.NewSessionStartHookTarget(anchor)
	if err != nil {
		t.Fatal(err)
	}
	matcher, _ := domain.ParseSessionStartHookScope("*")
	declaration, err := domain.NewSessionStartHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "context", matcher, "cat context.txt", 5)
	if err != nil {
		t.Fatal(err)
	}
	host := &fakeHookSession{}
	model := NewModel(context.Background(), "test", host)
	model.activeTurnID = turn
	model.refreshState()
	model.width, model.height = 100, 24
	model.composer = composerWithText(t, "draft to keep")
	emit := func(event protocol.Event, err error) {
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
	emit(protocol.NewCompactionStarted(session, thread, id, 1, scope))
	emit(protocol.NewCompactionSummarizing(session, thread, id, scope))
	post := protocol.PostCompactInstalledPayload{Scope: scope, CheckpointSequence: 8, Revision: 2, PlanDigest: domain.HookDigest([]byte("post")), Count: 1}
	emit(protocol.NewPostCompactInstalled(session, thread, id, post))
	planning := protocol.SessionStartPayload{Target: anchor, PlanDigest: domain.HookDigest([]byte("context")), State: protocol.SessionStartPlanned, Code: domain.HookOK, Count: 1}
	emit(protocol.NewSessionStart(session, thread, planning))
	if model.compaction.sessionStart.present() {
		t.Fatal("session context advanced before PostCompact")
	}
	emit(protocol.NewPostCompactClosed(session, thread, id, protocol.PostCompactClosedPayload{
		Scope: scope, CheckpointSequence: 8, PlanDigest: post.PlanDigest, CloseSequence: 9,
		State: protocol.PostCompactSucceeded, Code: domain.HookOK, Finished: 1,
	}))
	emit(protocol.NewSessionStart(session, thread, planning))
	review := protocol.HookTrustRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000025", Target: target, PlanDigest: post.PlanDigest, Declaration: declaration}
	emit(protocol.NewHookTrustRequested(review))
	if model.hookTrust != nil {
		t.Fatal("PostCompact digest authorized SessionStart")
	}
	review.PlanDigest = planning.PlanDigest
	emit(protocol.NewHookTrustRequested(review))
	if model.hookTrust == nil || model.hookTrust.trust {
		t.Fatal("expected a new review with default denial")
	}
	for _, part := range []string{"Triggered by: compact (auto)", "Matcher: *", "later matching triggers", "saved in Session", declaration.Command} {
		if !strings.Contains(model.renderHookTrust(), part) {
			t.Fatalf("review omitted %q", part)
		}
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 19, Height: 5})
	model, command := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || !model.hookReviewUnavailable() {
		t.Fatal("incomplete review accepted")
	}
	model, command = updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if command == nil || !strings.Contains(model.compactionStatus, "Conversation compacted · cancelling SessionStart") {
		t.Fatal("current cancellation lost the installed checkpoint")
	}
	command()
	if host.interrupts != 1 {
		t.Fatal("automatic compaction did not cancel the active turn")
	}
	stale, _ := protocol.NewHookTrustResponse(session, thread, turn, review, protocol.HookTrustSession)
	emit(protocol.NewHookTrustResolved(stale))
	if model.hookTrust == nil {
		t.Fatal("late review ended cleanup")
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 24})
	closed := updateModel(t, model, eventStreamClosedMsg{})
	for _, part := range []string{"Conversation compacted", "PostCompact passed", "context outcome uncertain"} {
		if !strings.Contains(closed.View(), part) {
			t.Fatalf("control closure lost %q: %s", part, closed.View())
		}
	}
	closed, command = updateModelWithCommand(t, closed, tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || closed.hookTrust != nil || closed.composer.text != "draft to keep" {
		t.Fatal("closed SessionStart accepted input or lost its draft")
	}
}

type sessionStartBrowserQueries struct {
	browserQueries
	through uint64
}

func (q *sessionStartBrowserQueries) ListTranscript(ctx context.Context, query protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	page, err := q.browserQueries.ListTranscript(ctx, query)
	q.through = query.Through
	if query.Through >= 9 {
		item, itemErr := protocol.NewSessionStartTranscriptListItem(protocol.TranscriptCursor{Sequence: 9}, domain.SessionStartTarget{
			Source: domain.SessionStartResume, TurnID: "018f1d8a-7b5c-7def-8123-456789abcdea",
		})
		if itemErr != nil {
			return protocol.TranscriptListPage{}, itemErr
		}
		page.Items = append([]protocol.TranscriptListItem{item}, page.Items...)
	}
	return page, err
}

func TestSessionStartSavedContextAdvancesOpenTranscriptWithoutMovingAnchor(t *testing.T) {
	model, _, _ := browserModel(t)
	queries := &sessionStartBrowserQueries{}
	model.browser.queries = queries
	model.main.sequence = 2
	model, command := browserKey(t, model, tea.KeyCtrlO)
	stale := command()
	model = updateModel(t, model, stale)
	anchor := model.browser.items[0].Cursor
	next, command := model.refreshSessionStartTranscript(9)
	model = next.(Model)
	model = updateModel(t, model, stale)
	if command == nil || model.browser.items[0].Cursor != anchor {
		t.Fatal("kept page changed before refresh")
	}
	model = updateModel(t, model, command())
	if queries.through != 9 || model.browser.items[model.browser.selected].Cursor != anchor || model.composer.text != "kept draft" ||
		!strings.Contains(model.renderTranscriptBrowser(), "SessionStart (resume) · Saved model context") {
		t.Fatal("new transcript did not retain the current reading anchor and saved source")
	}
	model, _ = browserKey(t, model, tea.KeyUp)
	model, command = browserKey(t, model, tea.KeyEnter)
	model = updateModel(t, model, command())
	if !strings.Contains(model.renderTranscriptBrowser(), "frozen new") || strings.Contains(model.renderTranscriptBrowser(), "input/result/saved") {
		t.Fatal("saved context did not use the standalone text detail")
	}
}

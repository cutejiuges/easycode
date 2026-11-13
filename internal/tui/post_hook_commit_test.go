package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"strings"
	"testing"
)

type postBrowserQueries struct {
	browserQueries
	sequence uint64
}

func (q *postBrowserQueries) ReadTranscriptBody(ctx context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	if query.Source != protocol.TranscriptPostHooks {
		return q.browserQueries.ReadTranscriptBody(ctx, query)
	}
	if query.DetailSequence != q.sequence {
		return protocol.TranscriptBodyPage{}, protocol.TranscriptChanged
	}
	return protocol.TranscriptBodyPage{DetailSequence: q.sequence, Text: "new explicit feedback", NextOffset: 21, TotalBytes: 21}, nil
}
func TestOpenPostDetailsRefreshesFirstPageAndDiscardsOldRevision(t *testing.T) {
	for _, withReasoning := range []bool{false, true} {
		t.Run(map[bool]string{false: "tool-only", true: "with-reasoning"}[withReasoning], func(t *testing.T) { testOpenPostDetailsRefresh(t, withReasoning) })
	}
}
func testOpenPostDetailsRefresh(t *testing.T, withReasoning bool) {
	m, _, _ := browserModel(t)
	queries := &postBrowserQueries{sequence: 21}
	m.browser.queries = queries
	list, err := queries.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptDetail})
	if err != nil {
		t.Fatal(err)
	}
	item := list.Items[0]
	item.DetailSequence = 20
	m.browser.items = detailItems([]protocol.TranscriptListItem{item})
	m.browser.selected = 0
	if withReasoning {
		reasoning := protocol.TranscriptListItem{Cursor: protocol.TranscriptCursor{Sequence: item.Cursor.Sequence + 1}, Kind: domain.TranscriptReasoning, TurnID: item.TurnID, Text: "visible", Reasoning: &protocol.ReasoningSummaryPayload{Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable, TotalBytes: 7}}
		m.browser.items = detailItems([]protocol.TranscriptListItem{reasoning, item})
		m.browser.selected = 1
	}
	m.detailOpen = true
	m.browser.detail = true
	m.browser.source = protocol.TranscriptPostHooks
	m.activeTurnID = item.TurnID
	m.browser.offset = 400
	m.browser.previous = []uint64{0}
	m.browser.generation = 7
	oldKey := transcriptPageKey{kind: transcriptBodyPage, cursor: item.Cursor, source: protocol.TranscriptPostHooks, detailSequence: 20, offset: 400}
	oldPage := cachedTranscriptPage{key: oldKey, body: protocol.TranscriptBodyPage{DetailSequence: 20, Offset: 400, NextOffset: 403, TotalBytes: 403, Text: "old"}}
	stablePage := cachedTranscriptPage{key: transcriptPageKey{kind: transcriptBodyPage, cursor: item.Cursor, source: protocol.TranscriptResult}, body: protocol.TranscriptBodyPage{Text: "stable", NextOffset: 6, TotalBytes: 6}}
	m.browser.page = &oldPage
	m.browser.cache = []cachedTranscriptPage{oldPage, stablePage}
	event, err := protocol.NewPostHookCommitted(protocol.PostHookCommittedPayload{InvocationID: item.Activity.InvocationID, PlanDigest: strings.Repeat("a", 64), DetailSequence: 21, Phase: protocol.PostHookFinished})
	if err != nil {
		t.Fatal(err)
	}
	event.TurnID = item.TurnID
	updated, cmd := m.projectHookEvent(event)
	m = updated.(Model)
	if cmd == nil || m.browser.generation <= 7 || m.browser.offset != 0 || len(m.browser.previous) != 0 || m.browser.items[m.browser.selected].Cursor != item.Cursor || m.composer.text != "kept draft" {
		t.Fatal("post refresh lost source position or draft")
	}
	if len(m.browser.cache) != 1 || m.browser.cache[0].key != stablePage.key {
		t.Fatal("post refresh retained stale page or evicted unrelated body")
	}
	oldReply := transcriptReply{generation: 7, page: oldPage}
	updated, _ = m.receiveTranscriptPage(oldReply)
	m = updated.(Model)
	if m.browser.page != nil {
		t.Fatal("old revision reply overwrote refreshed page")
	}
	updated, _ = m.receiveTranscriptPage(cmd().(transcriptReply))
	m = updated.(Model)
	if m.browser.page == nil || m.browser.page.body.DetailSequence != 21 || !strings.Contains(m.browser.page.body.Text, "new explicit") {
		t.Fatal("new post first page not loaded")
	}
	mixed := *m.browser.page
	mixed.body.DetailSequence = 20
	updated, _ = m.receiveTranscriptPage(transcriptReply{generation: m.browser.generation, page: mixed})
	m = updated.(Model)
	if m.browser.errorText != "Invalid transcript body" || m.browser.page.body.DetailSequence != 21 {
		t.Fatal("mixed revision page accepted")
	}
	for i := 0; i < 8; i++ {
		p := stablePage
		p.key.offset = uint64(i + 100)
		m.browser.remember(p)
	}
	if len(m.browser.cache) > 4 {
		t.Fatal("post details exceeded four page cache")
	}
}
func TestPostCheckingAndAggregatePreserveToolSuccess(t *testing.T) {
	testPostCheckingAndAggregate(t, domain.HookPostToolUse, "success")
}
func TestFailureCheckingAndAggregatePreserveToolError(t *testing.T) {
	testPostCheckingAndAggregate(t, domain.HookPostToolUseFailure, "error")
}
func TestFailureCheckingAndAggregatePreserveToolConflict(t *testing.T) {
	testPostCheckingAndAggregate(t, domain.HookPostToolUseFailure, "conflict")
}
func testPostCheckingAndAggregate(t *testing.T, selected domain.HookEvent, original string) {
	m, _, _ := browserModel(t)
	turn := domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdea")
	id := "018f1d8a-7b5c-7def-8123-456789abcdeb"
	m.activeTurnID = turn
	activity := protocol.ToolActivityPayload{Tool: protocol.ToolWrite, InvocationID: id, Summary: "a.go", Status: original, Code: "ok"}
	m.transcript = []transcriptMessage{{activity: &activity}}
	m.toolItems = map[string]int{id: 0}
	m.toolClosed = map[string]bool{id: true}
	post, _ := domain.NewHookDeclaration(domain.HookSourceProject, selected, domain.HookFeedbackModel, strings.Repeat("a", 64), "post", 7, "true", 5)
	event, _ := protocol.NewPostHookCommitted(protocol.PostHookCommittedPayload{InvocationID: id, PlanDigest: strings.Repeat("b", 64), DetailSequence: 5, Phase: protocol.PostHookStarted})
	event.TurnID = turn
	updated, _ := m.projectHookEvent(event)
	m = updated.(Model)
	checking, _ := protocol.NewHookActivity(protocol.HookActivityPayload{Target: testHookTarget(t, id, domain.HookWrite), PlanDigest: strings.Repeat("b", 64), Reference: post.Reference, State: protocol.HookActivityChecking, Code: domain.HookOK})
	checking.TurnID = turn
	updated, _ = m.projectHookEvent(checking)
	m = updated.(Model)
	if m.transcript[0].hook == nil || m.transcript[0].hook.State != protocol.HookActivityChecking || m.transcript[0].activity.Status != original {
		t.Fatal("post checking discarded after tool terminal")
	}
	completed, _ := protocol.NewHookActivity(protocol.HookActivityPayload{Target: testHookTarget(t, id, domain.HookWrite), PlanDigest: strings.Repeat("b", 64), Reference: post.Reference, State: protocol.HookActivityCompleted, Code: domain.HookExitFailed})
	completed.TurnID = turn
	updated, _ = m.projectHookEvent(completed)
	m = updated.(Model)
	if !strings.Contains(strings.Join(renderTranscript(m.transcript, 80), "\n"), "checks failed") || m.transcript[0].activity.Status != original {
		t.Fatal("aggregate check failure rewrote/hid original success", m.View())
	}
	pre, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "pre", 7, "true", 5)
	stale, _ := protocol.NewHookActivity(protocol.HookActivityPayload{Target: testHookTarget(t, id, domain.HookWrite), PlanDigest: strings.Repeat("b", 64), Reference: pre.Reference, State: protocol.HookActivityChecking, Code: domain.HookOK})
	stale.TurnID = turn
	updated, _ = m.projectHookEvent(stale)
	m = updated.(Model)
	if m.transcript[0].hook.State != protocol.HookActivityCompleted {
		t.Fatal("old pre phase reopened closed tool")
	}
	stale, _ = protocol.NewHookActivity(protocol.HookActivityPayload{Target: testHookTarget(t, id, domain.HookWrite), PlanDigest: strings.Repeat("b", 64), Reference: pre.Reference, State: protocol.HookActivityState(domain.HookBlock), Code: domain.HookDenied})
	stale.TurnID = turn
	updated, _ = m.projectHookEvent(stale)
	m = updated.(Model)
	if m.transcript[0].hook.State != protocol.HookActivityCompleted || m.transcript[0].hook.Reference.Event != selected {
		t.Fatal("late pre terminal overwrote post summary")
	}
}

func TestPostActivitiesCannotSelectAnEventBeforeOriginalResult(t *testing.T) {
	m, _, _ := browserModel(t)
	turn := domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdea")
	id := "018f1d8a-7b5c-7def-8123-456789abcdeb"
	m.activeTurnID = turn
	activity := protocol.ToolActivityPayload{Tool: protocol.ToolWrite, InvocationID: id, Summary: "a.go", Status: "running"}
	m.transcript = []transcriptMessage{{activity: &activity}}
	m.toolItems = map[string]int{id: 0}
	m.toolClosed = map[string]bool{}
	planned, _ := protocol.NewPostHookCommitted(protocol.PostHookCommittedPayload{InvocationID: id, PlanDigest: strings.Repeat("b", 64), DetailSequence: 5, Phase: protocol.PostHookPlanned})
	planned.TurnID = turn
	updated, _ := m.projectHookEvent(planned)
	m = updated.(Model)
	for _, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure} {
		d, _ := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackModel, strings.Repeat("a", 64), "post", 7, "true", 5)
		checking, _ := protocol.NewHookActivity(protocol.HookActivityPayload{Target: testHookTarget(t, id, domain.HookWrite), PlanDigest: strings.Repeat("b", 64), Reference: d.Reference, State: protocol.HookActivityChecking, Code: domain.HookOK})
		checking.TurnID = turn
		updated, _ := m.projectHookEvent(checking)
		m = updated.(Model)
		if m.transcript[0].hook != nil || m.transcript[0].activity.Status != "running" || m.postHooks[id].Phase != protocol.PostHookPlanned {
			t.Fatal("unknown original result selected a post event")
		}
	}
}

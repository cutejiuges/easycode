package tui

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type stopBrowserQueries struct {
	browserQueries
	binding  protocol.StopHookQueryTarget
	sequence uint64
}

func (q *stopBrowserQueries) ReadTranscriptBody(ctx context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	if query.Source != protocol.TranscriptStopHooks {
		return q.browserQueries.ReadTranscriptBody(ctx, query)
	}
	if query.StopHook == nil || *query.StopHook != q.binding || query.DetailSequence != q.sequence {
		return protocol.TranscriptBodyPage{}, protocol.TranscriptChanged
	}
	return protocol.TranscriptBodyPage{DetailSequence: q.sequence, Text: "frozen completion details", NextOffset: 25, TotalBytes: 25}, nil
}
func TestStopHistoryInvalidatesOnlyBoundPagesAndRejectsLateReplies(t *testing.T) {
	m, _, _ := browserModel(t)
	m.activeTurnID = "01900000-0000-7000-8000-000000000001"
	target := m.stopTestTarget(t)
	binding := protocol.StopHookQueryTarget{Target: target, PlanDigest: domain.HookDigest(nil)}
	queries := &stopBrowserQueries{binding: binding, sequence: 21}
	m.browser.queries = queries
	old := protocol.StopHookCommittedPayload{Target: target, PlanDigest: binding.PlanDigest, DetailSequence: 20, Phase: protocol.StopHookDecided, Disposition: domain.StopContinued}
	item := protocol.TranscriptListItem{Cursor: protocol.TranscriptCursor{Sequence: 6}, Kind: domain.TranscriptAssistant, TurnID: m.activeTurnID, Text: "saved answer", DetailSequence: 20, StopHook: &old}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	m.browser.items = detailItems([]protocol.TranscriptListItem{item})
	m.browser.selected = 0
	m.detailOpen = true
	m.browser.detail = true
	m.browser.source = protocol.TranscriptStopHooks
	m.browser.generation = 7
	m.browser.offset = 400
	m.browser.previous = []uint64{0}
	m.stopCommit = &old
	key := transcriptPageKey{kind: transcriptBodyPage, cursor: item.Cursor, source: protocol.TranscriptStopHooks, detailSequence: 20, stopTarget: target, stopPlanDigest: binding.PlanDigest, offset: 400}
	stale := cachedTranscriptPage{key: key, body: protocol.TranscriptBodyPage{DetailSequence: 20, Offset: 400, NextOffset: 403, TotalBytes: 403, Text: "old"}}
	stable := cachedTranscriptPage{key: transcriptPageKey{kind: transcriptBodyPage, cursor: item.Cursor, source: protocol.TranscriptText}, body: protocol.TranscriptBodyPage{Text: "saved answer", NextOffset: 12, TotalBytes: 12}}
	m.browser.cache = []cachedTranscriptPage{stale, stable}
	m.browser.page = &stale
	m.composer.text = "kept draft"
	p := old
	p.DetailSequence = 21
	p.Phase = protocol.StopHookDelivered
	event, _ := protocol.NewStopHookCommitted(p)
	event.TurnID = m.activeTurnID
	updated, cmd := m.projectHookEvent(event)
	m = updated.(Model)
	if cmd == nil || m.browser.generation <= 7 || m.browser.offset != 0 || len(m.browser.previous) != 0 || m.composer.text != "kept draft" || len(m.browser.cache) != 1 || m.browser.cache[0].key != stable.key {
		t.Fatal("Stop refresh lost anchor, draft or bounded cache")
	}
	updated, _ = m.receiveTranscriptPage(transcriptReply{generation: 7, page: stale})
	m = updated.(Model)
	if m.browser.page != nil {
		t.Fatal("late old page installed")
	}
	updated, _ = m.receiveTranscriptPage(cmd().(transcriptReply))
	m = updated.(Model)
	if m.browser.page == nil || m.browser.page.body.DetailSequence != 21 || m.browser.errorText != "" {
		t.Fatal("fresh bound page missing", m.browser.errorText)
	}
	for range 8 {
		extra := stable
		extra.key.offset = uint64(len(m.browser.cache) + 100)
		m.browser.remember(extra)
	}
	if len(m.browser.cache) > 4 {
		t.Fatal("Stop escaped global page bound")
	}
	mismatch := p
	mismatch.PlanDigest = domain.HookDigest([]byte("other"))
	mismatch.DetailSequence++
	event, _ = protocol.NewStopHookCommitted(mismatch)
	event.TurnID = m.activeTurnID
	updated, _ = m.projectHookEvent(event)
	next := updated.(Model)
	if next.stopCommit.PlanDigest != p.PlanDigest || next.stopCommit.DetailSequence != 21 {
		t.Fatal("wrong plan replaced current history")
	}
}

func TestStopFeedbackNeverEntersUserRecallOrMergesByText(t *testing.T) {
	turn := domain.TurnID("01900000-0000-7000-8000-000000000001")
	user, _ := domain.NewSemanticUserMessage(domain.InputID(turn), "real user")
	d, _ := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "completion", "true", 5)
	var messages []domain.SemanticMessage
	messages = append(messages, user)
	var items []protocol.TranscriptListItem
	for index := uint32(0); index < 2; index++ {
		plan, _ := domain.NewStopChecksPlan(domain.AssistantSampleReference{TurnID: turn, SampleIndex: index}, uint8(index), []domain.HookReference{d.Reference})
		id, _ := domain.NewStopHookRunIdentity(plan, 0)
		candidate, _ := domain.NewHookFeedbackCandidate("same feedback")
		feedback, _ := domain.NewStopFeedback(domain.HookRunResult{Identity: id, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, d.Reference, candidate)
		message, _ := domain.NewSemanticHookMessage(feedback)
		messages = append(messages, message)
		source, _ := domain.NewHookMessageSource(feedback)
		items = append([]protocol.TranscriptListItem{{Kind: domain.TranscriptHook, TurnID: turn, Cursor: protocol.TranscriptCursor{Sequence: uint64(10 + index)}, Text: feedback.Text, HookSource: &source}}, items...)
	}
	history := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{{TurnID: turn, Messages: messages}}}
	m := NewModel(context.Background(), "test", nil).WithInputHistory(history)
	if len(m.composer.history.entries) != 1 || m.composer.history.entries[0] != "real user" {
		t.Fatal("Hook became user recall")
	}
	updated, _ := m.receiveMainTranscript(mainTranscriptReply{generation: m.main.generation, page: protocol.TranscriptListPage{Items: items}})
	m = updated.(Model)
	visible := m.visibleTranscript()
	if len(visible) != 2 || visible[0].role != "Hook" || visible[1].role != "Hook" || visible[0].cursor == visible[1].cursor || visible[0].text != visible[1].text {
		t.Fatal("same-text feedback sources merged")
	}
}

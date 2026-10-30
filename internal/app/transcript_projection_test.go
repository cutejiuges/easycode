package app

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

func TestTranscriptJoinPreservesNativeTextCallTextAndRejectsBrokenAnchors(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	entry := queryTestEntry(t, thread, 1)
	before, _ := domain.NewTranscriptAssistant(domain.TranscriptPosition{Entry: 0, Item: 0}, entry.TurnID, "before")
	anchor, _ := domain.NewTranscriptCall(domain.TranscriptPosition{Entry: 0, Item: 1}, entry.TurnID, string(entry.Invocation.ProviderCallID()), "exec_command")
	after, _ := domain.NewTranscriptAssistant(domain.TranscriptPosition{Entry: 0, Item: 2}, entry.TurnID, "after")
	view := domain.TranscriptView{Provider: domain.ProviderOpenAI, Items: []domain.TranscriptItem{before, anchor, after}}
	facades := mustAppToolCatalog(t).Facades()
	nodes, err := joinTranscript(view, []uint64{1}, []session.ToolHistoryEntry{entry}, facades)
	if err != nil || len(nodes) != 3 || nodes[0].item.Text != "before" || nodes[1].toolIndex != 0 || nodes[2].item.Text != "after" {
		t.Fatalf("ordered join: %v", err)
	}
	for _, test := range []struct {
		name    string
		entries []session.ToolHistoryEntry
		view    domain.TranscriptView
	}{
		{"missing fact", nil, view},
		{"duplicate fact", []session.ToolHistoryEntry{entry, entry}, view},
		{"missing anchor", []session.ToolHistoryEntry{entry}, domain.TranscriptView{Provider: domain.ProviderOpenAI, Items: []domain.TranscriptItem{before, after}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := joinTranscript(test.view, []uint64{1}, test.entries, facades); err == nil {
				t.Fatal("broken join accepted")
			}
		})
	}
	wrong := entry
	wrong.CallIndex = 1
	if _, err := joinTranscript(view, []uint64{1}, []session.ToolHistoryEntry{wrong}, facades); err == nil {
		t.Fatal("call order accepted")
	}
	wrong = entry
	wrong.SampleIndex = 1
	if _, err := joinTranscript(view, []uint64{1}, []session.ToolHistoryEntry{wrong}, facades); err == nil {
		t.Fatal("sample order accepted")
	}
	view.Items[1].CallName = "read_file"
	if _, err := joinTranscript(view, []uint64{1}, []session.ToolHistoryEntry{entry}, facades); err == nil {
		t.Fatal("capability mismatch accepted")
	}
}

func TestTranscriptBodyForwardBackwardUTF8AndThroughSnapshot(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	entry := queryTestEntry(t, thread, 1)
	text := strings.Repeat("中文🙂", 20000)
	item, _ := domain.NewTranscriptAssistant(domain.TranscriptPosition{}, entry.TurnID, text)
	q := newTranscriptQueries(thread, nil, nil)
	q.nodes = []transcriptNode{{cursor: protocol.TranscriptCursor{Sequence: 1}, item: item, toolIndex: -1}}
	defer q.Close()
	query := protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: q.nodes[0].cursor, Source: protocol.TranscriptText, Limit: 64 << 10}
	var joined strings.Builder
	var last protocol.TranscriptBodyPage
	for {
		page, err := q.ReadTranscriptBody(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		joined.WriteString(page.Text)
		last = page
		if page.NextOffset == page.TotalBytes {
			break
		}
		query.Offset = page.NextOffset
	}
	if joined.String() != text {
		t.Fatal("body pagination lost bytes")
	}
	query.Offset = last.Offset
	query.Backward = true
	for query.Offset > 0 {
		page, err := q.ReadTranscriptBody(context.Background(), query)
		if err != nil {
			t.Fatal(err)
		}
		if page.NextOffset != query.Offset || page.Text != text[page.Offset:page.NextOffset] {
			t.Fatal("backward body skipped bytes")
		}
		query.Offset = page.Offset
	}
	q.nodes = append(q.nodes, transcriptNode{cursor: protocol.TranscriptCursor{Sequence: 3}, item: item, toolIndex: -1})
	page, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: thread, Through: 1, Limit: 50})
	if err != nil || len(page.Items) != 1 || page.Items[0].Cursor.Sequence != 1 {
		t.Fatal("future commit leaked into snapshot")
	}
}

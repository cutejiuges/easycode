package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestReasoningQueriesFilterBeforePagingAndKeepExactBodyRanges(t *testing.T) {
	ctx := context.Background()
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	turn := domain.TurnID(thread)
	q := newTranscriptQueries(thread, nil, nil)
	body, _ := domain.NewReasoningBody(domain.ReasoningOpenAISummary, []string{strings.Repeat("中文", 15000), " last "})
	for sequence := uint64(1); sequence <= 160; sequence++ {
		item, err := domain.NewTranscriptReasoning(domain.TranscriptPosition{}, turn, body)
		if sequence%10 == 0 {
			item, err = domain.NewTranscriptAssistant(domain.TranscriptPosition{}, turn, fmt.Sprintf("answer %d", sequence))
		}
		if err != nil {
			t.Fatal(err)
		}
		q.nodes = append(q.nodes, transcriptNode{cursor: protocol.TranscriptCursor{Sequence: sequence}, item: item, toolIndex: -1})
	}
	q.mainNodes = mainTranscriptNodes(q.nodes)
	for _, tc := range []struct {
		mode    protocol.TranscriptListMode
		before  uint64
		through uint64
		newer   bool
		want    []uint64
	}{
		{mode: protocol.TranscriptMain, want: []uint64{160, 150, 140}},
		{mode: protocol.TranscriptDetail, want: []uint64{160, 159, 158}},
		{mode: protocol.TranscriptMain, before: 100, newer: true, want: []uint64{130, 120, 110}},
		{mode: protocol.TranscriptMain, before: 140, want: []uint64{130, 120, 110}},
		{mode: protocol.TranscriptMain, through: 145, want: []uint64{140, 130, 120}},
	} {
		page, err := q.ListTranscript(ctx, protocol.TranscriptListQuery{Mode: tc.mode, ThreadID: thread, Before: protocol.TranscriptCursor{Sequence: tc.before}, Through: tc.through, Newer: tc.newer, Limit: 3})
		if err != nil || len(page.Items) != 3 {
			t.Fatal("list failed", err)
		}
		for i, want := range tc.want {
			if page.Items[i].Cursor.Sequence != want {
				t.Fatalf("mode %s item %d: want %d got %d", tc.mode, i, want, page.Items[i].Cursor.Sequence)
			}
		}
	}
	page, err := q.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: protocol.TranscriptCursor{Sequence: 1}, Source: protocol.TranscriptText, Offset: body.Len() - 6, Limit: 6})
	if err != nil || page.Text != " last " || page.TotalBytes != body.Len() {
		t.Fatal("reasoning range lost part boundaries", err)
	}
	unavailable, _ := domain.NewReasoningBody(domain.ReasoningOpenAISummary, nil)
	item, _ := domain.NewTranscriptReasoning(domain.TranscriptPosition{}, turn, unavailable)
	q.nodes[0].item = item
	if _, err := q.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: protocol.TranscriptCursor{Sequence: 1}, Source: protocol.TranscriptText, Limit: 10}); err == nil {
		t.Fatal("unavailable reasoning became readable")
	}
}

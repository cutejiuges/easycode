package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type queryTestReader struct {
	entered  chan struct{}
	finished chan struct{}
	calls    int
}

func (r *queryTestReader) ReadArtifact(ctx context.Context, _ tool.ReadArtifactInput) (tool.ArtifactWindow, error) {
	r.calls++
	close(r.entered)
	<-ctx.Done()
	close(r.finished)
	return tool.ArtifactWindow{}, ctx.Err()
}

func queryTestEntry(t *testing.T, thread domain.ThreadID, sequence uint64) session.ToolHistoryEntry {
	t.Helper()
	id, _ := tool.GenerateInvocationID()
	input, _ := tool.NewExecInput("printf test", "", 0)
	ready, _ := tool.NewExecReadyCall("call_test", input)
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	execInvocation, _ := invocation.Exec()
	stats, _ := tool.NewExecStreamStats(3, 3)
	meta, err := tool.NewExecResultMetadata("", 0, true, 1, stats, tool.ExecStreamStats{}, false, false, true)
	if err != nil {
		t.Fatal(err)
	}
	d, err := tool.NewArtifactDescriptor("0123456789abcdef0123456789abcdef", thread, id, tool.ArtifactStdout, 3, 3, strings.Repeat("a", 64), true)
	if err != nil {
		t.Fatal(err)
	}
	saving, _ := tool.NewArtifactSaving(tool.ArtifactComplete, "", d)
	none, _ := tool.NewArtifactSaving(tool.ArtifactNotNeeded, "", tool.ArtifactDescriptor{})
	meta, err = meta.WithOutputSaving(saving, none)
	if err != nil {
		t.Fatal(err)
	}
	preview, _ := tool.NewModelPreview("frozen preview")
	result, err := tool.NewExecInvocationResult(execInvocation, tool.ResultSuccess, "ok", preview, meta)
	if err != nil {
		t.Fatal(err)
	}
	return session.ToolHistoryEntry{Sequence: sequence * 2, NativeSequence: sequence*2 - 1, TurnID: domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdea"), SampleIndex: uint32(sequence - 1), Invocation: invocation, Result: result, HasResult: true}
}

func TestCommandQueryCursorAndIsolation(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	reader := &queryTestReader{}
	var entries []session.ToolHistoryEntry
	for i := uint64(1); i <= 53; i++ {
		entries = append(entries, queryTestEntry(t, thread, i))
	}
	q := newTranscriptQueries(thread, reader, entries)
	projector := bindQueryTestTranscript(t, q, entries)
	defer func() { _ = q.Close() }()
	page, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: thread, Limit: 50})
	if err != nil || len(page.Items) != 50 || page.Items[0].Cursor.Sequence != 105 || page.NextBefore.Sequence != 7 {
		t.Fatalf("first page: %+v %v", page, err)
	}
	page.Items[0].Activity.Summary = "mutated"
	newEntry := queryTestEntry(t, thread, 54)
	anchor, _ := domain.NewTranscriptCall(domain.TranscriptPosition{Entry: 53}, newEntry.TurnID, string(newEntry.Invocation.ProviderCallID()), "exec_command")
	projector.view.Items = append(projector.view.Items, anchor)
	ready := newEntry
	ready.HasResult = false
	ready.Result = tool.InvocationResult{}
	if err := q.NativeCommitted(newEntry.NativeSequence, []session.ToolHistoryEntry{ready}); err != nil {
		t.Fatal(err)
	}
	q.ToolResult(newEntry.Result)
	older, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: thread, Before: page.NextBefore, Limit: 50})
	if err != nil || len(older.Items) != 3 || older.Items[0].Cursor.Sequence != 5 || older.NextBefore.Valid() {
		t.Fatalf("stable cursor: %+v %v", older, err)
	}
	newest, _ := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: thread, Limit: 1})
	if newest.Items[0].Cursor.Sequence != 107 || newest.Items[0].Activity.Summary != "printf test" || !newest.Items[0].Stdout.Readable {
		t.Fatal("live durable facts missing")
	}
	body, err := q.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: newest.Items[0].Cursor, Source: protocol.TranscriptResult, Limit: 65536})
	if err != nil || body.Text != "frozen preview" {
		t.Fatal("frozen body missing")
	}
	wrong := thread
	wrong = "018f1d8a-7b5c-7def-8123-456789abcdee"
	_, err = q.ReadToolOutput(context.Background(), protocol.ToolOutputQuery{ThreadID: wrong, InvocationID: string(entries[0].Invocation.InvocationID()), Stream: protocol.ToolStdout, Limit: 3})
	if !errors.Is(err, tool.ArtifactReadUnauthorized) || reader.calls != 0 {
		t.Fatal("query crossed thread boundary")
	}
	_, err = q.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: thread, Limit: 51})
	if err == nil {
		t.Fatal("unbounded list accepted")
	}
}

func TestTranscriptQueriesCloseCancelsAndWaits(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	entry := queryTestEntry(t, thread, 1)
	reader := &queryTestReader{entered: make(chan struct{}), finished: make(chan struct{})}
	q := newTranscriptQueries(thread, reader, []session.ToolHistoryEntry{entry})
	query := protocol.ToolOutputQuery{ThreadID: thread, InvocationID: string(entry.Invocation.InvocationID()), Stream: protocol.ToolStdout, Limit: 3}
	done := make(chan error, 1)
	go func() { _, err := q.ReadToolOutput(context.Background(), query); done <- err }()
	<-reader.entered
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reader.finished:
	default:
		t.Fatal("Close abandoned active reader")
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("query: %v", err)
	}
	if _, err := q.ReadToolOutput(context.Background(), query); !errors.Is(err, tool.ArtifactReadUnavailable) {
		t.Fatal("closed service accepted query")
	}
}

type queryTestProjector struct{ view domain.TranscriptView }

func (p *queryTestProjector) ProjectTranscript() (domain.TranscriptView, error) {
	return domain.NewTranscriptView(p.view.Provider, p.view.Items)
}
func bindQueryTestTranscript(t *testing.T, q *transcriptQueries, entries []session.ToolHistoryEntry) *queryTestProjector {
	t.Helper()
	projector := &queryTestProjector{view: domain.TranscriptView{Provider: domain.ProviderOpenAI}}
	commits := make([]session.NativeCommitRecord, len(entries))
	for i, entry := range entries {
		anchor, err := domain.NewTranscriptCall(domain.TranscriptPosition{Entry: uint64(i)}, entry.TurnID, string(entry.Invocation.ProviderCallID()), "exec_command")
		if err != nil {
			t.Fatal(err)
		}
		projector.view.Items = append(projector.view.Items, anchor)
		commits[i].Sequence = entry.NativeSequence
	}
	if err := q.bindTranscript(projector, mustAppToolCatalog(t), commits); err != nil {
		t.Fatal(err)
	}
	return projector
}

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

func (r *queryTestReader) ReadArtifact(ctx context.Context, _ tool.ReadArtifactInput) (tool.ArtifactPage, error) {
	r.calls++
	close(r.entered)
	<-ctx.Done()
	close(r.finished)
	return tool.ArtifactPage{}, ctx.Err()
}

func queryTestEntry(t *testing.T, thread domain.ThreadID, sequence uint64) session.CommandHistoryEntry {
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
	return session.CommandHistoryEntry{Sequence: sequence, Invocation: invocation, Result: result, HasResult: true}
}

func TestCommandQueryCursorAndIsolation(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	reader := &queryTestReader{}
	var entries []session.CommandHistoryEntry
	for i := uint64(1); i <= 53; i++ {
		entries = append(entries, queryTestEntry(t, thread, i))
	}
	q := newCommandQueries(thread, reader, entries)
	defer func() { _ = q.Close() }()
	page, err := q.ListCommands(context.Background(), protocol.CommandListQuery{ThreadID: thread, Limit: 50})
	if err != nil || len(page.Items) != 50 || page.Items[0].Sequence != 53 || page.NextBefore != 4 {
		t.Fatalf("first page: %+v %v", page, err)
	}
	page.Items[0].Command = "mutated"
	newEntry := queryTestEntry(t, thread, 54)
	q.CommandReady(newEntry.Sequence, newEntry.Invocation)
	q.CommandResult(newEntry.Result)
	older, err := q.ListCommands(context.Background(), protocol.CommandListQuery{ThreadID: thread, Before: page.NextBefore, Limit: 50})
	if err != nil || len(older.Items) != 3 || older.Items[0].Sequence != 3 || older.NextBefore != 0 {
		t.Fatalf("stable cursor: %+v %v", older, err)
	}
	newest, _ := q.ListCommands(context.Background(), protocol.CommandListQuery{ThreadID: thread, Limit: 1})
	if newest.Items[0].Sequence != 54 || newest.Items[0].Preview != "frozen preview" || !newest.Items[0].Stdout.Readable {
		t.Fatal("live durable facts missing")
	}
	wrong := thread
	wrong = "018f1d8a-7b5c-7def-8123-456789abcdee"
	_, err = q.ReadCommandOutput(context.Background(), protocol.CommandOutputQuery{ThreadID: wrong, InvocationID: string(entries[0].Invocation.InvocationID()), Stream: protocol.CommandStdout, Limit: 3})
	if !errors.Is(err, tool.ArtifactReadUnauthorized) || reader.calls != 0 {
		t.Fatal("query crossed thread boundary")
	}
	_, err = q.ListCommands(context.Background(), protocol.CommandListQuery{ThreadID: thread, Limit: 51})
	if err == nil {
		t.Fatal("unbounded list accepted")
	}
}

func TestCommandQueriesCloseCancelsAndWaits(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	entry := queryTestEntry(t, thread, 1)
	reader := &queryTestReader{entered: make(chan struct{}), finished: make(chan struct{})}
	q := newCommandQueries(thread, reader, []session.CommandHistoryEntry{entry})
	query := protocol.CommandOutputQuery{ThreadID: thread, InvocationID: string(entry.Invocation.InvocationID()), Stream: protocol.CommandStdout, Limit: 3}
	done := make(chan error, 1)
	go func() { _, err := q.ReadCommandOutput(context.Background(), query); done <- err }()
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
	if _, err := q.ReadCommandOutput(context.Background(), query); !errors.Is(err, tool.ArtifactReadUnavailable) {
		t.Fatal("closed service accepted query")
	}
}

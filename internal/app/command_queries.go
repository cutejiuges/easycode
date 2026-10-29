package app

import (
	"context"
	"errors"
	"sort"
	"sync"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// commandQueries 拥有当前线程查询与事实投影，关闭时取消并等待所有已接收查询。
type commandQueries struct {
	mu      sync.Mutex
	thread  domain.ThreadID
	reader  tool.ArtifactRangeReader
	entries []session.CommandHistoryEntry
	indices map[tool.InvocationID]int
	closed  bool
	next    uint64
	active  map[uint64]context.CancelFunc
	wait    sync.WaitGroup
}

func newCommandQueries(thread domain.ThreadID, reader tool.ArtifactRangeReader, entries []session.CommandHistoryEntry) *commandQueries {
	q := &commandQueries{thread: thread, reader: reader, indices: make(map[tool.InvocationID]int), active: make(map[uint64]context.CancelFunc)}
	q.entries = append([]session.CommandHistoryEntry(nil), entries...)
	for i, e := range q.entries {
		q.indices[e.Invocation.InvocationID()] = i
	}
	return q
}

func (q *commandQueries) CommandReady(sequence uint64, invocation tool.Invocation) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.indices[invocation.InvocationID()]; exists {
		return
	}
	q.indices[invocation.InvocationID()] = len(q.entries)
	q.entries = append(q.entries, session.CommandHistoryEntry{Sequence: sequence, Invocation: invocation})
}

func (q *commandQueries) CommandResult(result tool.InvocationResult) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if i, exists := q.indices[result.InvocationID()]; exists {
		q.entries[i].Result = result
		q.entries[i].HasResult = true
	}
}

func (q *commandQueries) begin(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	child, cancel := context.WithCancel(ctx)
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		cancel()
		return nil, nil, tool.ArtifactReadUnavailable
	}
	q.next++
	id := q.next
	q.active[id] = cancel
	q.wait.Add(1)
	q.mu.Unlock()
	return child, func() { cancel(); q.mu.Lock(); delete(q.active, id); q.mu.Unlock(); q.wait.Done() }, nil
}

func (q *commandQueries) Close() error {
	q.mu.Lock()
	q.closed = true
	cancels := make([]context.CancelFunc, 0, len(q.active))
	for _, cancel := range q.active {
		cancels = append(cancels, cancel)
	}
	q.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	q.wait.Wait()
	return nil
}

func (q *commandQueries) ListCommands(ctx context.Context, query protocol.CommandListQuery) (protocol.CommandListPage, error) {
	if query.Validate() != nil || query.ThreadID != q.thread {
		return protocol.CommandListPage{}, tool.ArtifactReadUnauthorized
	}
	ctx, done, err := q.begin(ctx)
	if err != nil {
		return protocol.CommandListPage{}, err
	}
	defer done()
	q.mu.Lock()
	entries := make([]session.CommandHistoryEntry, 0, query.Limit+1)
	end := len(q.entries)
	if query.Before != 0 {
		end = sort.Search(end, func(i int) bool { return q.entries[i].Sequence >= query.Before })
	}
	for i := end - 1; i >= 0 && len(entries) <= int(query.Limit); i-- {
		entries = append(entries, q.entries[i])
	}
	q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return protocol.CommandListPage{}, err
	}
	page := protocol.CommandListPage{}
	if len(entries) > int(query.Limit) {
		entries = entries[:query.Limit]
		page.NextBefore = entries[len(entries)-1].Sequence
	}
	for _, entry := range entries {
		page.Items = append(page.Items, commandHistoryItem(entry))
	}
	return page, nil
}

func commandHistoryItem(entry session.CommandHistoryEntry) protocol.CommandHistoryItem {
	invocation, _ := entry.Invocation.Exec()
	input := invocation.Input()
	item := protocol.CommandHistoryItem{Sequence: entry.Sequence, InvocationID: string(entry.Invocation.InvocationID()), Command: input.Command(), Workdir: input.Workdir(), Status: "waiting", Terminal: entry.HasResult}
	if entry.HasResult {
		r := entry.Result
		m := r.ExecMetadata()
		item.Status, item.Code, item.Preview, item.Truncated = string(r.Status()), r.Code(), r.Preview().Text(), m.OutputTruncated()
		item.ExitCode, item.ExitKnown = m.ExitCode()
		item.DurationMillis = m.DurationMillis()
		item.Stdout = commandOutputSummary(m.StdoutSaving(), m.Stdout().ObservedBytes())
		item.Stderr = commandOutputSummary(m.StderrSaving(), m.Stderr().ObservedBytes())
	}
	return item
}

func commandOutputSummary(s tool.ArtifactSaving, observed int64) protocol.CommandOutputSummary {
	v := protocol.CommandOutputSummary{Status: string(s.Status()), Reason: s.Reason(), ObservedBytes: observed}
	if d, ok := s.Descriptor(); ok {
		v.Readable = true
		v.SavedBytes = d.SavedBytes()
		v.Complete = d.Complete()
	}
	return v
}

func (q *commandQueries) ReadCommandOutput(ctx context.Context, query protocol.CommandOutputQuery) (protocol.CommandOutputPage, error) {
	if query.Validate() != nil || query.ThreadID != q.thread {
		return protocol.CommandOutputPage{}, tool.ArtifactReadUnauthorized
	}
	id, err := tool.ParseInvocationID(query.InvocationID)
	if err != nil {
		return protocol.CommandOutputPage{}, tool.ArtifactReadUnauthorized
	}
	ctx, done, err := q.begin(ctx)
	if err != nil {
		return protocol.CommandOutputPage{}, err
	}
	defer done()
	q.mu.Lock()
	index, exists := q.indices[id]
	var entry session.CommandHistoryEntry
	if exists {
		entry = q.entries[index]
	}
	q.mu.Unlock()
	if !exists || !entry.HasResult {
		return protocol.CommandOutputPage{}, tool.ArtifactReadUnauthorized
	}
	saving := entry.Result.ExecMetadata().StdoutSaving()
	if query.Stream == protocol.CommandStderr {
		saving = entry.Result.ExecMetadata().StderrSaving()
	}
	d, ok := saving.Descriptor()
	if !ok {
		return protocol.CommandOutputPage{}, tool.ArtifactReadUnavailable
	}
	input, err := tool.NewReadArtifactInput(d.ID(), query.Offset, query.Limit)
	if err != nil {
		return protocol.CommandOutputPage{}, tool.ArtifactReadInvalidRange
	}
	page, err := q.reader.ReadArtifact(ctx, input)
	if err != nil {
		for _, safe := range []error{context.Canceled, context.DeadlineExceeded, tool.ArtifactReadUnauthorized, tool.ArtifactReadCorrupt, tool.ArtifactReadInvalidRange} {
			if errors.Is(err, safe) {
				return protocol.CommandOutputPage{}, safe
			}
		}
		return protocol.CommandOutputPage{}, tool.ArtifactReadUnavailable
	}
	return protocol.CommandOutputPage{Offset: page.Offset(), NextOffset: page.NextOffset(), StoredBytes: page.SavedBytes(), StoredEOF: page.EOF(), SourceComplete: page.SourceComplete(), Text: page.Text()}, nil
}

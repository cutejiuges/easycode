package app

import (
	"context"
	"errors"
	"sync"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// transcriptQueries 拥有当前线程查询与事实投影，关闭时取消并等待所有已接收查询。
type transcriptQueries struct {
	projector       provider.TranscriptProjector
	facades         []tool.Facade
	nativeSequences []uint64
	nodes           []transcriptNode
	mu              sync.Mutex
	thread          domain.ThreadID
	reader          tool.ArtifactRangeReader
	entries         []session.ToolHistoryEntry
	indices         map[tool.InvocationID]int
	closed          bool
	invalid         bool
	next            uint64
	active          map[uint64]context.CancelFunc
	wait            sync.WaitGroup
}

func newTranscriptQueries(thread domain.ThreadID, reader tool.ArtifactRangeReader, entries []session.ToolHistoryEntry) *transcriptQueries {
	q := &transcriptQueries{thread: thread, reader: reader, indices: make(map[tool.InvocationID]int), active: make(map[uint64]context.CancelFunc)}
	q.entries = append([]session.ToolHistoryEntry(nil), entries...)
	for i, e := range q.entries {
		q.indices[e.Invocation.InvocationID()] = i
	}
	return q
}

func (q *transcriptQueries) ToolResult(result tool.InvocationResult) {
	invalid := tool.ValidateFrozenResult(result) != nil
	q.mu.Lock()
	defer q.mu.Unlock()
	index, exists := q.indices[result.InvocationID()]
	if invalid || !exists {
		q.invalid = true
		return
	}
	entry := q.entries[index]
	if entry.HasResult || entry.Invocation.ProviderCallID() != result.ProviderCallID() || entry.Invocation.Capability() != result.Capability() || entry.Invocation.WireKind() != result.WireKind() {
		q.invalid = true
		return
	}
	q.entries[index].Result = result
	q.entries[index].HasResult = true
}

func (q *transcriptQueries) begin(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, tool.ArtifactReadUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	child, cancel := context.WithCancel(ctx)
	q.mu.Lock()
	if q.closed || q.invalid {
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

func (q *transcriptQueries) Close() error {
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

func toolOutputSummary(s tool.ArtifactSaving, observed int64) protocol.ToolOutputSummary {
	return tool.ProjectOutputSaving(s, observed).Summary
}

func (q *transcriptQueries) ReadToolOutput(ctx context.Context, query protocol.ToolOutputQuery) (protocol.ToolOutputPage, error) {
	if query.Validate() != nil || query.ThreadID != q.thread {
		return protocol.ToolOutputPage{}, tool.ArtifactReadUnauthorized
	}
	id, err := tool.ParseInvocationID(query.InvocationID)
	if err != nil {
		return protocol.ToolOutputPage{}, tool.ArtifactReadUnauthorized
	}
	ctx, done, err := q.begin(ctx)
	if err != nil {
		return protocol.ToolOutputPage{}, err
	}
	defer done()
	q.mu.Lock()
	index, exists := q.indices[id]
	var entry session.ToolHistoryEntry
	if exists {
		entry = q.entries[index]
	}
	q.mu.Unlock()
	if !exists || !entry.HasResult {
		return protocol.ToolOutputPage{}, tool.ArtifactReadUnauthorized
	}
	var saving tool.ArtifactSaving
	switch query.Stream {
	case protocol.ToolStdout, protocol.ToolStderr:
		if entry.Invocation.Capability() != tool.CapabilityExec {
			return protocol.ToolOutputPage{}, tool.ArtifactReadUnauthorized
		}
		saving = entry.Result.ExecMetadata().StdoutSaving()
		if query.Stream == protocol.ToolStderr {
			saving = entry.Result.ExecMetadata().StderrSaving()
		}
	case protocol.ToolSearchResult:
		if entry.Invocation.Capability() != tool.CapabilityGlob && entry.Invocation.Capability() != tool.CapabilityGrep {
			return protocol.ToolOutputPage{}, tool.ArtifactReadUnauthorized
		}
		saving = entry.Result.SearchSaving()
	default:
		return protocol.ToolOutputPage{}, tool.ArtifactReadUnauthorized
	}
	d, ok := saving.Descriptor()
	if !ok {
		return protocol.ToolOutputPage{}, tool.ArtifactReadUnavailable
	}
	offset, limit := query.Offset, query.Limit
	if query.Backward {
		limit = min(offset, min(limit, uint64((tool.MaxModelPreviewBytes-2048)/4)))
		offset -= limit
	}
	input, err := tool.NewReadArtifactInput(d.ID(), offset, limit)
	if err != nil {
		return protocol.ToolOutputPage{}, tool.ArtifactReadInvalidRange
	}
	window, err := q.reader.ReadArtifact(ctx, input)
	var page tool.ArtifactPage
	if err == nil {
		if query.Backward {
			page, err = window.RenderBefore(query.Offset, tool.MaxModelPreviewBytes)
		} else {
			page, err = window.Render(tool.MaxModelPreviewBytes)
		}
	}
	if err != nil {
		for _, safe := range []error{context.Canceled, context.DeadlineExceeded, tool.ArtifactReadUnauthorized, tool.ArtifactReadCorrupt, tool.ArtifactReadInvalidRange} {
			if errors.Is(err, safe) {
				return protocol.ToolOutputPage{}, safe
			}
		}
		return protocol.ToolOutputPage{}, tool.ArtifactReadUnavailable
	}
	return protocol.ToolOutputPage{Offset: page.Offset(), NextOffset: page.NextOffset(), StoredBytes: page.SavedBytes(), StoredEOF: page.EOF(), SourceComplete: page.SourceComplete(), Text: page.Text()}, nil
}

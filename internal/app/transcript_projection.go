package app

import (
	"errors"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type transcriptNode struct {
	compaction *compactionTranscript
	cursor     protocol.TranscriptCursor
	item       domain.TranscriptItem
	tool       *session.ToolHistoryEntry
}

type transcriptCallKey struct {
	sequence uint64
	call     tool.ProviderCallID
}

type transcriptAppend struct {
	nodes   []transcriptNode
	inputs  map[domain.InputID]struct{}
	samples map[domain.TurnID]uint32
}

// joinTranscript 只连接新投影与其 ready 事实，历史索引只用于按身份查找。
func (q *transcriptQueries) joinTranscript(view domain.TranscriptView, start uint64, sequences []uint64, entries []*session.ToolHistoryEntry) (transcriptAppend, error) {
	if view.Provider != q.family || view.Validate() != nil {
		return transcriptAppend{}, errors.New("invalid native transcript projection")
	}
	last := q.lastSequence
	for _, seq := range sequences {
		if seq <= last {
			return transcriptAppend{}, errors.New("invalid native transcript sequence")
		}
		last = seq
	}
	anchors := make(map[transcriptCallKey]*session.ToolHistoryEntry, len(entries))
	for _, entry := range entries {
		key := transcriptCallKey{entry.NativeSequence, entry.Invocation.ProviderCallID()}
		if entry.Invocation.Validate() != nil || entry.Sequence <= entry.NativeSequence || !entry.TurnID.Valid() {
			return transcriptAppend{}, errors.New("invalid transcript tool fact")
		}
		if _, exists := anchors[key]; exists {
			return transcriptAppend{}, errors.New("duplicate transcript tool fact")
		}
		anchors[key] = entry
	}
	suffix := transcriptAppend{
		nodes:   make([]transcriptNode, 0, len(view.Items)),
		inputs:  make(map[domain.InputID]struct{}),
		samples: make(map[domain.TurnID]uint32),
	}
	var previousEntry uint64
	var callIndex, sampleIndex uint32
	var callTurn domain.TurnID
	for _, item := range view.Items {
		if item.Position.Entry < start || item.Position.Entry-start >= uint64(len(sequences)) {
			return transcriptAppend{}, errors.New("transcript native entry is missing")
		}
		if item.Position.Entry != previousEntry {
			previousEntry, callIndex = item.Position.Entry, 0
		}
		sequence := sequences[item.Position.Entry-start]
		node := transcriptNode{cursor: protocol.TranscriptCursor{Sequence: sequence, Item: item.Position.Item}, item: item}
		switch item.Kind {
		case domain.TranscriptUser:
			if _, exists := q.inputs[item.InputID]; exists {
				return transcriptAppend{}, errors.New("duplicate transcript input")
			}
			suffix.inputs[item.InputID] = struct{}{}
		case domain.TranscriptHook:
			sample, _ := item.HookSource.Identity.Target.AssistantSample()
			suffix.samples[item.TurnID] = sample.SampleIndex + 1
		case domain.TranscriptCall:
			key := transcriptCallKey{sequence, tool.ProviderCallID(item.CallID)}
			entry := anchors[key]
			if entry == nil {
				return transcriptAppend{}, errors.New("transcript call anchor is missing or duplicated")
			}
			if callIndex == 0 {
				next, updated := suffix.samples[item.TurnID]
				if !updated {
					next = q.samples[item.TurnID]
				}
				sampleIndex, callTurn = next, item.TurnID
				suffix.samples[item.TurnID] = next + 1
			}
			if entry.SampleIndex != sampleIndex {
				return transcriptAppend{}, errors.New("transcript sample order mismatch")
			}
			if item.TurnID != callTurn || entry.TurnID != callTurn || entry.CallIndex != callIndex {
				return transcriptAppend{}, errors.New("transcript call order mismatch")
			}
			facade := q.facades[item.CallName]
			if facade.Capability() != entry.Invocation.Capability() || facade.WireKind() != entry.Invocation.WireKind() {
				return transcriptAppend{}, errors.New("transcript call capability mismatch")
			}
			callIndex++
			delete(anchors, key)
			node.tool = entry
		}
		suffix.nodes = append(suffix.nodes, node)
	}
	if len(anchors) != 0 {
		return transcriptAppend{}, errors.New("tool fact has no native transcript anchor")
	}
	return suffix, nil
}

func (q *transcriptQueries) bindTranscript(projector provider.TranscriptProjector, catalog tool.CatalogSnapshot, commits []session.NativeCommitRecord, facts []session.ToolHistoryEntry, compactions []compactionTranscript) error {
	if projector == nil || catalog.Validate() != nil {
		return errors.New("transcript projector is required")
	}
	sequences := make([]uint64, len(commits))
	for i, commit := range commits {
		sequences[i] = commit.Sequence
	}
	view, end, err := projector.ProjectTranscriptFrom(0)
	if err != nil {
		return err
	}
	if end != uint64(len(sequences)) {
		return errors.New("transcript native commit count mismatch")
	}
	q.family = view.Provider
	q.facades = make(map[string]tool.Facade)
	for _, facade := range catalog.Facades() {
		if facade.Family() == q.family {
			q.facades[facade.Name()] = facade
		}
	}
	entries, err := q.prepareTranscriptTools(facts)
	if err != nil {
		return err
	}
	initial, err := q.joinTranscript(view, 0, sequences, entries)
	if err != nil {
		return err
	}
	if len(sequences) != 0 {
		q.lastSequence = sequences[len(sequences)-1]
	}
	q.compactionIDs = make(map[domain.CompactionID]struct{}, len(compactions))
	for _, boundary := range compactions {
		q.compactionIDs[boundary.id] = struct{}{}
		q.lastSequence = max(q.lastSequence, boundary.sequence)
	}
	q.projector, q.nativeOrdinal = projector, end
	q.inputs, q.samples = initial.inputs, initial.samples
	for _, entry := range entries {
		q.tools[entry.Invocation.InvocationID()] = entry
	}
	q.nodes.restored = mergeCompactionTranscript(initial.nodes, compactions)
	q.mainNodes.restored = mainTranscriptIndices(q.nodes.restored, 0)
	return nil
}

// NativeCommitted 由唯一 Runtime owner 在 finalize 后发布一个原生提交与对应 ready 批次。
func (q *transcriptQueries) NativeCommitted(sequence uint64, ready []session.ToolHistoryEntry) error {
	return q.nativeBatchCommitted([]uint64{sequence}, ready)
}

// StopFeedbackCommitted 一次发布一或两个已原子 finalize 的原生位置。
func (q *transcriptQueries) StopFeedbackCommitted(sequences []uint64) error {
	return q.nativeBatchCommitted(sequences, nil)
}

func (q *transcriptQueries) nativeBatchCommitted(batch []uint64, ready []session.ToolHistoryEntry) error {
	q.mu.Lock()
	unavailable := q.closed || q.invalid
	q.mu.Unlock()
	if unavailable || len(batch) == 0 {
		return errors.New("invalid transcript facts")
	}

	// 只有 Runtime owner 写入关联索引，其他消费者在锁内读取已发布的条目。
	start := q.nativeOrdinal
	view, end, err := q.projector.ProjectTranscriptFrom(start)
	if err != nil {
		return err
	}
	if end < start || end-start != uint64(len(batch)) {
		return errors.New("transcript native commit count mismatch")
	}
	entries, err := q.prepareTranscriptTools(ready)
	if err != nil {
		return err
	}
	suffix, err := q.joinTranscript(view, start, batch, entries)
	if err != nil {
		return err
	}
	mainIndices := mainTranscriptIndices(suffix.nodes, q.nodes.len())
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.invalid {
		return errors.New("invalid transcript facts")
	}
	for _, entry := range entries {
		q.tools[entry.Invocation.InvocationID()] = entry
	}
	for id := range suffix.inputs {
		q.inputs[id] = struct{}{}
	}
	for turn, next := range suffix.samples {
		q.samples[turn] = next
	}
	q.nodes.append(suffix.nodes)
	q.mainNodes.append(mainIndices)
	q.nativeOrdinal, q.lastSequence = end, batch[len(batch)-1]
	return nil
}

// prepareTranscriptTools 独立持有事实并保留 journal 给出的顺序。
func (q *transcriptQueries) prepareTranscriptTools(facts []session.ToolHistoryEntry) ([]*session.ToolHistoryEntry, error) {
	entries := make([]*session.ToolHistoryEntry, 0, len(facts))
	seen := make(map[tool.InvocationID]struct{}, len(facts))
	for _, entry := range facts {
		id := entry.Invocation.InvocationID()
		if _, duplicate := seen[id]; duplicate || q.tools[id] != nil {
			return nil, errors.New("duplicate transcript invocation")
		}
		seen[id] = struct{}{}
		entry.Invocation = entry.Invocation.Clone()
		if entry.PreHooks != nil {
			hooks := entry.PreHooks.Clone()
			entry.PreHooks = &hooks
		}
		entry.Hooks = append([]session.HookHistoryEntry(nil), entry.Hooks...)
		if entry.PostHooks != nil {
			hooks := entry.PostHooks.Clone()
			entry.PostHooks = &hooks
		}
		entries = append(entries, &entry)
	}
	return entries, nil
}

// mainTranscriptIndices 只为可见主线条目保存索引。
func mainTranscriptIndices(nodes []transcriptNode, start int) []int {
	result := make([]int, 0, len(nodes))
	for index, node := range nodes {
		if node.item.Kind != domain.TranscriptReasoning {
			result = append(result, start+index)
		}
	}
	return result
}

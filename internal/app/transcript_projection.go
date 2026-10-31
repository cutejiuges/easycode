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
	toolIndex  int
}

type transcriptCallKey struct {
	sequence uint64
	call     tool.ProviderCallID
}

// joinTranscript 只连接已有强类型投影与验证后的 ready 事实，不解码 Provider wire。
func joinTranscript(view domain.TranscriptView, sequences []uint64, entries []session.ToolHistoryEntry, facades []tool.Facade) ([]transcriptNode, error) {
	if view.Validate() != nil {
		return nil, errors.New("invalid native transcript projection")
	}
	for i, seq := range sequences {
		if seq == 0 || (i > 0 && seq <= sequences[i-1]) {
			return nil, errors.New("invalid native transcript sequence")
		}
	}
	indices := make(map[transcriptCallKey]int, len(entries))
	for i, entry := range entries {
		key := transcriptCallKey{entry.NativeSequence, entry.Invocation.ProviderCallID()}
		if entry.Invocation.Validate() != nil || entry.Sequence <= entry.NativeSequence || !entry.TurnID.Valid() {
			return nil, errors.New("invalid transcript tool fact")
		}
		if _, exists := indices[key]; exists {
			return nil, errors.New("duplicate transcript tool fact")
		}
		indices[key] = i
	}
	nodes := make([]transcriptNode, 0, len(view.Items))
	seen := make(map[int]bool, len(entries))
	callIndices := make(map[uint64]uint32)
	samples := make(map[domain.TurnID]uint32)
	for _, item := range view.Items {
		if item.Position.Entry >= uint64(len(sequences)) {
			return nil, errors.New("transcript native entry is missing")
		}
		sequence := sequences[item.Position.Entry]
		node := transcriptNode{cursor: protocol.TranscriptCursor{Sequence: sequence, Item: item.Position.Item}, item: item, toolIndex: -1}
		if item.Kind == domain.TranscriptCall {
			index, ok := indices[transcriptCallKey{sequence, tool.ProviderCallID(item.CallID)}]
			if !ok || seen[index] {
				return nil, errors.New("transcript call anchor is missing or duplicated")
			}
			entry := entries[index]
			expected := samples[item.TurnID]
			if callIndices[sequence] == 0 {
				samples[item.TurnID] = expected + 1
			} else {
				expected--
			}
			if entry.SampleIndex != expected {
				return nil, errors.New("transcript sample order mismatch")
			}
			if entry.TurnID != item.TurnID || entry.CallIndex != callIndices[sequence] {
				return nil, errors.New("transcript call order mismatch")
			}
			matched := false
			for _, facade := range facades {
				if facade.Family() == view.Provider && facade.Name() == item.CallName && facade.Capability() == entry.Invocation.Capability() && facade.WireKind() == entry.Invocation.WireKind() {
					matched = true
					break
				}
			}
			if !matched {
				return nil, errors.New("transcript call capability mismatch")
			}
			callIndices[sequence]++
			seen[index] = true
			node.toolIndex = index
		}
		nodes = append(nodes, node)
	}
	if len(seen) != len(entries) {
		return nil, errors.New("tool fact has no native transcript anchor")
	}
	return nodes, nil
}

func (q *transcriptQueries) bindTranscript(projector provider.TranscriptProjector, catalog tool.CatalogSnapshot, commits []session.NativeCommitRecord) error {
	if projector == nil || catalog.Validate() != nil {
		return errors.New("transcript projector is required")
	}
	sequences := make([]uint64, len(commits))
	for i, commit := range commits {
		sequences[i] = commit.Sequence
	}
	view, err := projector.ProjectTranscript()
	if err != nil {
		return err
	}
	nodes, err := joinTranscript(view, sequences, q.entries, catalog.Facades())
	if err != nil {
		return err
	}
	q.projector = projector
	q.facades = catalog.Facades()
	q.nativeSequences = sequences
	q.nodes = mergeCompactionTranscript(nodes, q.compactions)
	return nil
}

// NativeCommitted 由唯一 Runtime owner 在 finalize 后发布一个原生提交与对应 ready 批次。
func (q *transcriptQueries) NativeCommitted(sequence uint64, ready []session.ToolHistoryEntry) error {
	q.mu.Lock()
	if q.invalid {
		q.mu.Unlock()
		return errors.New("invalid transcript facts")
	}
	entries, sequences, compactions := q.entries, q.nativeSequences, q.compactions
	q.mu.Unlock()
	entries = append(append([]session.ToolHistoryEntry(nil), entries...), ready...)
	sequences = append(append([]uint64(nil), sequences...), sequence)
	view, err := q.projector.ProjectTranscript()
	if err != nil {
		return err
	}
	nodes, err := joinTranscript(view, sequences, entries, q.facades)
	if err != nil {
		return err
	}
	indices := make(map[tool.InvocationID]int, len(entries))
	for i, entry := range entries {
		if _, exists := indices[entry.Invocation.InvocationID()]; exists {
			return errors.New("duplicate transcript invocation")
		}
		indices[entry.Invocation.InvocationID()] = i
	}
	nodes = mergeCompactionTranscript(nodes, compactions)
	q.mu.Lock()
	q.entries, q.nativeSequences, q.nodes, q.indices = entries, sequences, nodes, indices
	q.mu.Unlock()
	return nil
}

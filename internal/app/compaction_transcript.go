package app

import (
	"fmt"
	"sort"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

type compactionTranscript struct {
	sequence uint64
	id       domain.CompactionID
	summary  domain.ContextSummary
}

func restoreCompactionTranscript(factory provider.Factory, plan session.ReplayPlan) ([]compactionTranscript, error) {
	values := make([]compactionTranscript, 0)
	for _, operation := range plan.Compactions {
		if operation.Completed == nil {
			continue
		}
		projector, ok := factory.(provider.CheckpointProjector)
		if !ok {
			return nil, fmt.Errorf("checkpoint projection is unavailable")
		}
		summary, err := projector.ProjectCheckpoint(operation.Completed.Checkpoint)
		if err != nil {
			return nil, err
		}
		values = append(values, compactionTranscript{sequence: operation.TerminalSequence, id: operation.Started.ID, summary: summary})
	}
	return values, nil
}

func mergeCompactionTranscript(nodes []transcriptNode, compactions []compactionTranscript) []transcriptNode {
	result := append([]transcriptNode{}, nodes...)
	for _, boundary := range compactions {
		result = append(result, transcriptNode{cursor: protocol.TranscriptCursor{Sequence: boundary.sequence}, compaction: &boundary, toolIndex: -1})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].cursor.Before(result[j].cursor) })
	return result
}

// CompactionCommitted 发布独立 seq 边界，不占用 native ordinal 或工具锚点。
func (q *transcriptQueries) CompactionCommitted(sequence uint64, id domain.CompactionID, summary domain.ContextSummary) error {
	if sequence == 0 || !id.Valid() {
		return fmt.Errorf("compaction transcript identity is invalid")
	}
	if _, err := domain.NewContextSummary(summary.Text()); err != nil {
		return err
	}
	q.mu.Lock()
	existing, nodes := q.compactions, q.nodes
	invalid := q.invalid || q.closed
	q.mu.Unlock()
	if invalid {
		return fmt.Errorf("compaction transcript is unavailable")
	}
	for _, boundary := range existing {
		if boundary.sequence >= sequence || boundary.id == id {
			return fmt.Errorf("compaction transcript order is invalid")
		}
	}
	if len(nodes) > 0 && nodes[len(nodes)-1].cursor.Sequence >= sequence {
		return fmt.Errorf("compaction transcript sequence is invalid")
	}
	boundary := compactionTranscript{sequence: sequence, id: id, summary: summary}
	next := append(append([]compactionTranscript{}, existing...), boundary)
	updated := append(append([]transcriptNode{}, nodes...), transcriptNode{cursor: protocol.TranscriptCursor{Sequence: sequence}, compaction: &boundary, toolIndex: -1})
	q.mu.Lock()
	q.compactions, q.nodes = next, updated
	q.mu.Unlock()
	return nil
}

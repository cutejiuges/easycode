package app

import (
	"context"
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

func restoreCompactionTranscript(ctx context.Context, projector provider.CheckpointReader, plan session.ReplayPlan) ([]compactionTranscript, error) {
	values := make([]compactionTranscript, 0)
	for _, operation := range plan.Compactions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if operation.Completed == nil {
			continue
		}
		if projector == nil {
			return nil, fmt.Errorf("checkpoint projection is unavailable")
		}
		summary, err := projector.ProjectCheckpoint(operation.Completed.Checkpoint)
		if err != nil {
			return nil, err
		}
		values = append(values, compactionTranscript{sequence: operation.TerminalSequence, id: operation.Started.ID, summary: summary})
	}
	return values, ctx.Err()
}

func mergeCompactionTranscript(nodes []transcriptNode, compactions []compactionTranscript) []transcriptNode {
	if len(compactions) == 0 {
		return nodes
	}
	result := make([]transcriptNode, 0, len(nodes)+len(compactions))
	result = append(result, nodes...)
	for _, boundary := range compactions {
		result = append(result, transcriptNode{cursor: protocol.TranscriptCursor{Sequence: boundary.sequence}, compaction: &boundary})
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
	defer q.mu.Unlock()
	if q.invalid || q.closed {
		return fmt.Errorf("compaction transcript is unavailable")
	}
	if _, exists := q.compactionIDs[id]; exists || q.lastSequence >= sequence {
		return fmt.Errorf("compaction transcript order is invalid")
	}
	boundary := compactionTranscript{sequence: sequence, id: id, summary: summary}
	node := transcriptNode{cursor: protocol.TranscriptCursor{Sequence: sequence}, compaction: &boundary}
	q.mainNodes.append([]int{q.nodes.len()})
	q.nodes.append([]transcriptNode{node})
	q.compactionIDs[id] = struct{}{}
	q.lastSequence = sequence
	return nil
}

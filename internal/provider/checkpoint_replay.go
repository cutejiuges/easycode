package provider

import (
	"bytes"
	"fmt"

	"easycode/internal/domain"
)

// DecodedCheckpoint 保存一次原生解码所得的替换条目及查验事实。
// 解码后续字段失败时，仍保留已经解析出的 Tasks 供回放按原顺序报错。
type DecodedCheckpoint[E any] struct {
	Replacement E
	Summary     domain.ContextSummary
	Tasks       domain.TaskProgressState
	Usage       domain.SampleUsage
}

type checkpointRead[E any] struct {
	envelope CheckpointEnvelope
	decoded  DecodedCheckpoint[E]
	err      error
}

// checkpointReplay 只在一次在线恢复或离线预览内复用解码结果。
type checkpointReplay[E any] struct {
	model   string
	decode  func(CheckpointEnvelope) (DecodedCheckpoint[E], error)
	entries map[domain.CompactionID]checkpointRead[E]
}

func (r *checkpointReplay[E]) read(envelope CheckpointEnvelope) (DecodedCheckpoint[E], error) {
	if entry, ok := r.entries[envelope.id]; ok {
		if entry.envelope.source != envelope.source || entry.envelope.scope != envelope.scope ||
			!bytes.Equal(entry.envelope.native.payload, envelope.native.payload) {
			return DecodedCheckpoint[E]{}, fmt.Errorf("checkpoint evidence does not match envelope")
		}
		return entry.decoded, entry.err
	}
	decoded, err := r.decode(envelope)
	if r.entries == nil {
		r.entries = make(map[domain.CompactionID]checkpointRead[E])
	}
	r.entries[envelope.id] = checkpointRead[E]{envelope, decoded, err}
	return decoded, err
}

func (r *checkpointReplay[E]) ValidateCheckpointUsage(envelope CheckpointEnvelope, expected domain.SampleUsage) error {
	value, err := r.read(envelope)
	if err != nil {
		return err
	}
	if value.Usage != expected {
		return fmt.Errorf("checkpoint usage does not match provenance")
	}
	return nil
}

func (r *checkpointReplay[E]) ProjectCheckpoint(envelope CheckpointEnvelope) (domain.ContextSummary, error) {
	if envelope.source.Model() != r.model {
		return domain.ContextSummary{}, fmt.Errorf("checkpoint model mismatch")
	}
	value, err := r.read(envelope)
	return value.Summary, err
}

package provider

import (
	"encoding/json"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

type predecessorRecord struct {
	Root         bool                `json:"root"`
	CheckpointID domain.CompactionID `json:"checkpoint_id,omitempty"`
}

type compactionSourceRecord struct {
	Family            domain.ProviderFamily `json:"provider"`
	Wire              string                `json:"wire"`
	Model             string                `json:"model"`
	CommittedRevision uint64                `json:"committed_revision"`
	ContextRevision   uint64                `json:"context_revision"`
	Previous          predecessorRecord     `json:"previous"`
	Digest            string                `json:"digest"`
	PromptRevision    string                `json:"prompt_revision"`
}

// MarshalJSON 编码完整来源身份，root 状态必须明确。
func (source CompactionSource) MarshalJSON() ([]byte, error) {
	if err := source.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(compactionSourceRecord{Family: source.family, Wire: source.wire, Model: source.model, CommittedRevision: source.committedRevision, ContextRevision: source.contextRevision, Previous: predecessorRecord{Root: source.previous.IsRoot(), CheckpointID: source.previous.ID()}, Digest: source.digest, PromptRevision: source.promptRevision})
}

// UnmarshalJSON 严格校验来源字段并仅在全部成功后赋值。
func (source *CompactionSource) UnmarshalJSON(data []byte) error {
	var record compactionSourceRecord
	if err := codec.UnmarshalRecord(data, &record); err != nil {
		return fmt.Errorf("compaction source encoding is invalid")
	}
	previous := CompactionPredecessor{root: record.Previous.Root, id: record.Previous.CheckpointID}
	value, err := NewCompactionSource(record.Family, record.Wire, record.Model, record.CommittedRevision, record.ContextRevision, previous, record.Digest, record.PromptRevision)
	if err != nil {
		return err
	}
	*source = value
	return nil
}

type checkpointRecord struct {
	ID             domain.CompactionID `json:"compaction_id"`
	Source         CompactionSource    `json:"source"`
	PayloadVersion int                 `json:"payload_version"`
	Payload        json.RawMessage     `json:"payload"`
}

// MarshalJSON 只保存原生摘要一次，公共元数据不复制正文。
func (checkpoint CheckpointEnvelope) MarshalJSON() ([]byte, error) {
	if _, err := checkpoint.Clone(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(checkpointRecord{ID: checkpoint.ID(), Source: checkpoint.Source(), PayloadVersion: checkpoint.PayloadVersion(), Payload: checkpoint.Payload()})
}

// UnmarshalJSON 解码受控 opaque envelope；原生 shape 由对应 Provider 校验。
func (checkpoint *CheckpointEnvelope) UnmarshalJSON(data []byte) error {
	if len(data) > MaxNativeCommitBytes {
		return fmt.Errorf("checkpoint exceeds size limit")
	}
	var record checkpointRecord
	if err := codec.UnmarshalRecord(data, &record); err != nil {
		return fmt.Errorf("checkpoint encoding is invalid")
	}
	value, err := NewCheckpointEnvelope(record.ID, record.Source, record.PayloadVersion, record.Payload)
	if err != nil {
		return err
	}
	*checkpoint = value
	return nil
}

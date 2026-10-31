package session

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

// CompactionStartedPayload 冻结压缩开始时的已提交来源。
type CompactionStartedPayload struct {
	ID     domain.CompactionID       `json:"compaction_id"`
	Source provider.CompactionSource `json:"source"`
}

// CompactionCompletedPayload 原子保存独立 checkpoint、摘要计量和窗口估算。
type CompactionCompletedPayload struct {
	Checkpoint provider.CheckpointEnvelope     `json:"checkpoint"`
	Usage      SampleUsagePayload              `json:"usage"`
	Before     protocol.ContextEstimatePayload `json:"before"`
	After      protocol.ContextEstimatePayload `json:"after"`
}

// CompactionFailedPayload 保存唯一失败及有明确可用性的独立计量。
type CompactionFailedPayload struct {
	ID         domain.CompactionID            `json:"compaction_id"`
	Code       protocol.CompactionFailureCode `json:"code"`
	UsageState string                         `json:"usage_state"`
	Usage      *SampleUsagePayload            `json:"usage,omitempty"`
	Provenance *NativeCommitPayload           `json:"provenance,omitempty"`
}

// NewCompactionStartedDraft 在网络前创建必须 Sync 的独立操作事实。
func NewCompactionStartedDraft(id domain.CompactionID, source provider.CompactionSource) (RecordDraft, error) {
	payload := CompactionStartedPayload{ID: id, Source: source}
	if err := payload.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventCompactionStarted, "", "", payload)
}

// NewCompactionCompletedDraft 冻结成功候选，编码不安装内存窗口。
func NewCompactionCompletedDraft(checkpoint provider.CheckpointEnvelope, usage domain.SampleUsage, before, after domain.TokenEstimate) (RecordDraft, error) {
	usagePayload, err := NewSampleUsagePayload(usage)
	if err != nil {
		return RecordDraft{}, err
	}
	beforePayload, err := protocol.NewContextEstimatePayload(before)
	if err != nil {
		return RecordDraft{}, err
	}
	afterPayload, err := protocol.NewContextEstimatePayload(after)
	if err != nil {
		return RecordDraft{}, err
	}
	payload := CompactionCompletedPayload{Checkpoint: checkpoint, Usage: usagePayload, Before: beforePayload, After: afterPayload}
	if err := payload.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventCompactionCompleted, "", "", payload)
}

// NewCompactionFailedDraft 创建不可安装窗口的失败事实。
func NewCompactionFailedDraft(payload CompactionFailedPayload) (RecordDraft, error) {
	if err := payload.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventCompactionFailed, "", "", payload)
}

// Validate 校验操作身份和来源，拒绝把前驱重用为新操作。
func (payload CompactionStartedPayload) Validate() error {
	if !payload.ID.Valid() || payload.ID == payload.Source.Previous().ID() {
		return fmt.Errorf("compaction identity is invalid")
	}
	return payload.Source.Validate()
}

// Validate 校验原生 envelope、usage 及严格缩减条件。
func (payload CompactionCompletedPayload) Validate() error {
	if _, err := payload.Checkpoint.Clone(); err != nil {
		return err
	}
	if _, err := payload.Usage.Domain(); err != nil {
		return err
	}
	before, err := payload.Before.Domain()
	if err != nil {
		return err
	}
	after, err := payload.After.Domain()
	if err != nil {
		return err
	}
	old, oldKnown := before.Tokens()
	next, nextKnown := after.Tokens()
	if !oldKnown || !nextKnown || next >= old || before.Method() != after.Method() {
		return fmt.Errorf("compaction estimates are invalid")
	}
	return nil
}

// Validate 拒绝缺失或伪造的 unavailable/observed 字段组合。
func (payload CompactionFailedPayload) Validate() error {
	if !payload.ID.Valid() || !payload.Code.Valid() {
		return fmt.Errorf("compaction failure identity or code is invalid")
	}
	switch payload.UsageState {
	case "unavailable":
		if payload.Usage != nil || payload.Provenance != nil {
			return fmt.Errorf("unavailable compaction usage contains facts")
		}
	case "observed":
		if payload.Usage == nil || payload.Provenance == nil {
			return fmt.Errorf("observed compaction usage is incomplete")
		}
		if _, err := payload.Usage.Domain(); err != nil {
			return err
		}
		if err := validateNativeCommitPayload(*payload.Provenance); err != nil {
			return err
		}
		if len(payload.Provenance.Payload) > 64<<10 {
			return fmt.Errorf("compaction usage provenance exceeds size limit")
		}
	default:
		return fmt.Errorf("compaction usage state is invalid")
	}
	return nil
}

// DecodeCompactionStartedPayload 严格解码无 TurnID 的开始事实。
func DecodeCompactionStartedPayload(record Record) (CompactionStartedPayload, error) {
	payload, err := decodeKnownPayload[CompactionStartedPayload](record, EventCompactionStarted)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" {
		return payload, fmt.Errorf("compaction record contains turn identity")
	}
	return payload, payload.Validate()
}

// DecodeCompactionCompletedPayload 严格解码成功事实。
func DecodeCompactionCompletedPayload(record Record) (CompactionCompletedPayload, error) {
	payload, err := decodeKnownPayload[CompactionCompletedPayload](record, EventCompactionCompleted)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" {
		return payload, fmt.Errorf("compaction record contains turn identity")
	}
	return payload, payload.Validate()
}

// DecodeCompactionFailedPayload 严格解码失败事实。
func DecodeCompactionFailedPayload(record Record) (CompactionFailedPayload, error) {
	payload, err := decodeKnownPayload[CompactionFailedPayload](record, EventCompactionFailed)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" {
		return payload, fmt.Errorf("compaction record contains turn identity")
	}
	return payload, payload.Validate()
}

// ReplayedCompaction 保存独立边界的 Session 顺序及确定终态。
type ReplayedCompaction struct {
	StartedSequence  uint64
	TerminalSequence uint64
	Started          CompactionStartedPayload
	Completed        *CompactionCompletedPayload
	Failed           *CompactionFailedPayload
}

// compactionReplay 独占压缩语法，不扩大普通 turn 状态机。
type compactionReplay struct {
	active    *ReplayedCompaction
	seen      map[domain.CompactionID]struct{}
	previous  domain.CompactionID
	completed uint64
}

func (state *compactionReplay) accept(plan *ReplayPlan, batch []Record, turnActive bool) error {
	if len(batch) != 1 || turnActive {
		return replayCorrupt("compaction placement is invalid")
	}
	record := batch[0]
	switch record.EventKind {
	case EventCompactionStarted:
		payload, err := DecodeCompactionStartedPayload(record)
		if err != nil {
			return err
		}
		if state.active != nil {
			return replayCorrupt("compaction overlaps an active operation")
		}
		if _, exists := state.seen[payload.ID]; exists {
			return replayCorrupt("compaction identity is duplicated")
		}
		source := payload.Source
		if source.Family() != plan.SessionMetadata.Provider || source.Wire() != plan.SessionMetadata.ProviderWire || source.Model() != plan.SessionMetadata.Model || source.CommittedRevision() != uint64(len(plan.NativeCommits)) || source.ContextRevision() != uint64(len(plan.NativeCommits))+state.completed || source.Previous().ID() != state.previous || source.Previous().IsRoot() != (state.previous == "") {
			return replayCorrupt("compaction source metadata does not match journal")
		}
		state.seen[payload.ID] = struct{}{}
		state.active = &ReplayedCompaction{StartedSequence: record.Sequence, Started: payload}
	case EventCompactionCompleted:
		payload, err := DecodeCompactionCompletedPayload(record)
		if err != nil {
			return err
		}
		if state.active == nil || payload.Checkpoint.ID() != state.active.Started.ID || payload.Checkpoint.Source() != state.active.Started.Source {
			return replayCorrupt("compaction completion does not match start")
		}
		state.active.Completed = &payload
		state.previous = payload.Checkpoint.ID()
		state.completed++
	case EventCompactionFailed:
		payload, err := DecodeCompactionFailedPayload(record)
		if err != nil {
			return err
		}
		if state.active == nil || payload.ID != state.active.Started.ID {
			return replayCorrupt("compaction failure does not match start")
		}
		if payload.Provenance != nil && (payload.Provenance.Provider != state.active.Started.Source.Family() || payload.Provenance.Wire != state.active.Started.Source.Wire()) {
			return replayCorrupt("compaction usage provider mismatch")
		}
		state.active.Failed = &payload
	default:
		return replayCorrupt("invalid compaction record")
	}
	if record.EventKind != EventCompactionStarted {
		state.active.TerminalSequence = record.Sequence
		plan.Compactions = append(plan.Compactions, *state.active)
		state.active = nil
	}
	return nil
}

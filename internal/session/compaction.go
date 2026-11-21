package session

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

// CompactionStartedPayload 冻结压缩开始时的已提交来源。
type CompactionStartedPayload struct {
	Scope      domain.CompactionScope    `json:"scope"`
	ID         domain.CompactionID       `json:"compaction_id"`
	Source     provider.CompactionSource `json:"source"`
	PreCompact PreCompactPlan            `json:"pre_compact"`
}

// CompactionCompletedPayload 原子保存独立 checkpoint、摘要计量和窗口估算。
type CompactionCompletedPayload struct {
	Scope       domain.CompactionScope          `json:"scope"`
	Checkpoint  provider.CheckpointEnvelope     `json:"checkpoint"`
	Usage       SampleUsagePayload              `json:"usage"`
	Before      protocol.ContextEstimatePayload `json:"before"`
	After       protocol.ContextEstimatePayload `json:"after"`
	PostCompact PostCompactPlan                 `json:"post_compact"`
}

// CompactionFailedPayload 保存唯一失败及有明确可用性的独立计量。
type CompactionFailedPayload struct {
	Scope      domain.CompactionScope         `json:"scope"`
	ID         domain.CompactionID            `json:"compaction_id"`
	Code       protocol.CompactionFailureCode `json:"code"`
	UsageState string                         `json:"usage_state"`
	Usage      *SampleUsagePayload            `json:"usage,omitempty"`
	Provenance *NativeCommitPayload           `json:"provenance,omitempty"`
}

// NewCompactionStartedDraft 在审查、执行及网络前冻结压缩开始事实。
func NewCompactionStartedDraft(scope domain.CompactionScope, id domain.CompactionID, source provider.CompactionSource, plan PreCompactPlan) (RecordDraft, error) {
	payload := CompactionStartedPayload{Scope: scope, ID: id, Source: source, PreCompact: plan}
	if err := payload.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventCompactionStarted, "", "", payload)
}

// NewCompactionCompletedDraft 冻结成功候选，编码不安装内存窗口。
func NewCompactionCompletedDraft(scope domain.CompactionScope, checkpoint provider.CheckpointEnvelope, usage domain.SampleUsage, before, after domain.TokenEstimate, postCompact PostCompactPlan) (RecordDraft, error) {
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
	payload := CompactionCompletedPayload{Scope: scope, Checkpoint: checkpoint, Usage: usagePayload, Before: beforePayload, After: afterPayload, PostCompact: postCompact}
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
	if payload.ID == payload.Source.Previous().ID() {
		return fmt.Errorf("compaction identity is invalid")
	}
	return payload.PreCompact.validate(payload.ID, payload.Scope, payload.Source)
}

// Validate 校验原生 envelope、usage 及严格缩减条件。
func (payload CompactionCompletedPayload) Validate() error {
	if payload.Scope != payload.Checkpoint.Scope() {
		return fmt.Errorf("checkpoint scope mismatch")
	}
	if err := payload.Scope.Validate(); err != nil {
		return err
	}
	if _, err := payload.Checkpoint.Clone(); err != nil {
		return err
	}
	if err := payload.PostCompact.validate(payload.Checkpoint); err != nil {
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
	if err := payload.Scope.Validate(); err != nil {
		return err
	}
	if !payload.ID.Valid() || !payload.Code.Valid() {
		return fmt.Errorf("compaction failure identity or code is invalid")
	}
	if (payload.Code.IsHookFailure() || payload.Code == protocol.CompactionFailureInterrupted) && payload.UsageState != "unavailable" {
		return fmt.Errorf("compaction failure cannot have observed usage")
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
		if len(payload.Provenance.Payload) > domain.MaxCompactionUsageProvenanceBytes {
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
	if record.TurnID != "" || record.ParentThreadID != "" {
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
	taskProgress     replayedTaskProgress
}

// TaskProgressAtStart 返回压缩开始时的独立任务证据。
func (operation ReplayedCompaction) TaskProgressAtStart() (domain.TaskProgressState, error) {
	return operation.taskProgress.snapshot()
}

// compactionReplay 独占压缩语法，不扩大普通 turn 状态机。
type compactionReplay struct {
	active      *ReplayedCompaction
	preCompact  preCompactReplay
	postCompact *postCompactReplay
	seen        map[domain.CompactionID]struct{}
	previous    domain.CompactionID
	completed   uint64
}

func (state *compactionReplay) accept(plan *replayAccumulator, batch []Record, turn *activeReplayTurn) error {
	if len(batch) != 1 {
		return replayCorrupt("compaction placement is invalid")
	}
	record := batch[0]
	if state.postCompact != nil {
		if err := state.postCompact.accept(record); err != nil {
			return err
		}
		if record.EventKind == EventPostCompactHookClosed {
			state.postCompact = nil
		}
		return nil
	}
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
		if payload.Scope.Trigger == domain.CompactionManual {
			if turn != nil {
				return replayCorrupt("manual compaction overlaps turn")
			}
		} else {
			if turn == nil || payload.Scope.TurnID != turn.turnID || payload.Scope.SampleIndex != turn.nextSampleIndex || turn.inputAtSample || turn.autoCompactedSample || turn.pendingCalls != nil || (turn.stop != nil && !turn.stop.PendingFeedback()) || (turn.initialInputs && turn.stop == nil && (!turn.hadToolSample || !turn.toolOutputsCommitted)) {
				return replayCorrupt("automatic compaction boundary is invalid")
			}
			turn.autoCompactedSample = true
		}
		source := payload.Source
		if source.Family() != plan.SessionMetadata.Provider || source.Wire() != plan.SessionMetadata.ProviderWire || source.Model() != plan.SessionMetadata.Model || source.CommittedRevision() != plan.nativeRevision || source.ContextRevision() != plan.nativeRevision+state.completed || source.Previous().ID() != state.previous || source.Previous().IsRoot() != (state.previous == "") {
			return replayCorrupt("compaction source metadata does not match journal")
		}
		state.seen[payload.ID] = struct{}{}
		state.active = &ReplayedCompaction{StartedSequence: record.Sequence, Started: payload, taskProgress: plan.taskProgress}
		state.preCompact = preCompactReplay{}
		return nil
	case EventPreCompactHookStarted, EventPreCompactHookFinished, EventPreCompactHookDecided:
		if state.active == nil || state.active.Started.PreCompact.kind != preCompactPlanned {
			return replayCorrupt("pre-compact hook has no active plan")
		}
		return state.preCompact.accept(state.active.Started, record)
	case EventCompactionCompleted:
		payload, err := DecodeCompactionCompletedPayload(record)
		if err != nil {
			return err
		}
		if state.active == nil || payload.Checkpoint.ID() != state.active.Started.ID || payload.Checkpoint.Source() != state.active.Started.Source || payload.Scope != state.active.Started.Scope {
			return replayCorrupt("compaction completion does not match start")
		}
		if !state.preCompact.allowsSummary(state.active.Started.PreCompact) {
			return replayCorrupt("compaction completion has no matching hook permission")
		}
		state.active.Completed = &payload
		state.previous = payload.Checkpoint.ID()
		state.completed++
		if payload.PostCompact.kind == PostCompactPlanPlanned {
			target, err := domain.NewCompactionHookTarget(payload.Checkpoint.ID(), payload.Scope)
			if err != nil {
				return replayCorrupt("post-compact target is invalid")
			}
			state.postCompact = &postCompactReplay{target: target, plan: payload.PostCompact}
		}
	case EventCompactionFailed:
		payload, err := DecodeCompactionFailedPayload(record)
		if err != nil {
			return err
		}
		if state.active == nil || payload.ID != state.active.Started.ID || payload.Scope != state.active.Started.Scope {
			return replayCorrupt("compaction failure does not match start")
		}
		if payload.Provenance != nil && (payload.Provenance.Provider != state.active.Started.Source.Family() || payload.Provenance.Wire != state.active.Started.Source.Wire()) {
			return replayCorrupt("compaction usage provider mismatch")
		}
		if !state.preCompact.acceptsFailure(state.active.Started.PreCompact, payload) {
			return replayCorrupt("compaction failure does not match pre-compact evidence")
		}
		state.active.Failed = &payload
	default:
		return replayCorrupt("invalid compaction record")
	}
	state.active.TerminalSequence = record.Sequence
	if plan.projectHistory {
		plan.Compactions = append(plan.Compactions, *state.active)
	}
	state.active = nil
	state.preCompact = preCompactReplay{}
	return nil
}

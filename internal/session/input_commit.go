package session

import (
	"easycode/internal/domain"
	"fmt"
	"math"
	"time"
)

// TurnInputsPayload 仅记录输入归属，正文保存在紧邻的原生 commit。
type TurnInputsPayload struct {
	Placement         domain.InputPlacement `json:"placement"`
	TargetSampleIndex uint32                `json:"target_sample_index"`
	InputIDs          []domain.InputID      `json:"input_ids"`
}

// NewTurnInputsCommittedDraft 创建独立输入提交的关联事实。
func NewTurnInputsCommittedDraft(a domain.InputAssociation) (RecordDraft, error) {
	if err := a.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventTurnInputsCommitted, "", a.TurnID(), TurnInputsPayload{Placement: a.Placement(), TargetSampleIndex: a.SampleIndex(), InputIDs: a.IDs()})
}

// DecodeTurnInputsPayload 严格解码当前输入关联，拒绝额外正文和重复身份。
func DecodeTurnInputsPayload(record Record) (TurnInputsPayload, error) {
	payload, err := decodeKnownPayload[TurnInputsPayload](record, EventTurnInputsCommitted)
	if err != nil {
		return TurnInputsPayload{}, err
	}
	a, err := domain.NewInputAssociation(record.TurnID, payload.Placement, payload.TargetSampleIndex, payload.InputIDs)
	if err != nil {
		return TurnInputsPayload{}, err
	}
	payload.InputIDs = a.IDs()
	return payload, nil
}

func acceptInputBatch(plan *ReplayPlan, active *activeReplayTurn, batch []Record, seen map[domain.InputID]struct{}) error {
	if active == nil || len(batch) != 2 || !batchHasTurn(batch, active.turnID) || batch[1].EventKind != EventProviderNativeCommit || active.pendingCalls != nil {
		return replayCorrupt("input batch placement is invalid")
	}
	payload, err := DecodeTurnInputsPayload(batch[0])
	if err != nil {
		return replayCorrupt("input association is invalid")
	}
	if payload.Placement == domain.InputInitial {
		if active.initialInputs || active.nextSampleIndex != 0 {
			return replayCorrupt("initial inputs are duplicated")
		}
	} else if !active.initialInputs || !active.toolOutputsCommitted || active.inputAtSample || payload.TargetSampleIndex != active.nextSampleIndex {
		return replayCorrupt("steering input target is invalid")
	}
	for _, id := range payload.InputIDs {
		if _, exists := seen[id]; exists {
			return replayCorrupt("input ID is duplicated")
		}
	}
	commit, err := DecodeNativeCommitPayload(batch[1])
	if err != nil {
		return err
	}
	a, err := domain.NewInputAssociation(active.turnID, payload.Placement, payload.TargetSampleIndex, payload.InputIDs)
	if err != nil {
		return err
	}
	for _, id := range payload.InputIDs {
		seen[id] = struct{}{}
	}
	plan.NativeCommits = append(plan.NativeCommits, NativeCommitRecord{Sequence: batch[1].Sequence, TurnID: active.turnID, Role: domain.NativeCommitUserInputs, Inputs: a, Commit: cloneNativeCommit(commit)})
	active.initialInputs = true
	active.inputAtSample = true
	active.lastSequence = batch[1].Sequence
	return nil
}

// ValidateDraftBatchSize 在 writer 接收前按完整 envelope 最大开销验证编码预算。
func ValidateDraftBatchSize(drafts []RecordDraft) error {
	// 身份长度固定；使用最大数字宽度和纳秒时间，复用 writer 的唯一编码路径。
	const id = "00000000-0000-7000-8000-000000000001"
	identity := Identity{SessionID: domain.SessionID(id), ThreadID: domain.ThreadID(id)}
	timestamp := time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	total := 0
	for _, draft := range drafts {
		record, err := BuildRecord(identity, draft, math.MaxUint64, timestamp, math.MaxUint64, math.MaxUint32-1, math.MaxUint32)
		if err != nil {
			return err
		}
		_, encoded, err := EncodeRecord(record)
		if err != nil {
			return err
		}
		total += len(encoded) + 1
		if total > MaxBatchBytes {
			return fmt.Errorf("session batch exceeds size limit")
		}
	}
	return nil
}

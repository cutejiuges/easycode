package session

import (
	"fmt"
	"slices"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// PromptSubmitPlan 冻结一次真实输入 claim 的有序身份和匹配声明引用。
type PromptSubmitPlan struct {
	fields promptSubmitPlanFields
	digest string
}

type promptSubmitPlanFields struct {
	TurnID domain.TurnID `json:"turn_id"`
	TurnInputsPayload
	Checks []domain.HookReference `json:"checks"`
}

type promptSubmitPlanWire struct {
	promptSubmitPlanFields
	Digest string `json:"digest"`
}

// NewPromptSubmitPlan 只保存输入身份与声明引用，计划不含命令或提示词。
func NewPromptSubmitPlan(assoc domain.InputAssociation, declarations []domain.HookDeclaration) (PromptSubmitPlan, error) {
	checks := make([]domain.HookReference, len(declarations))
	for index, declaration := range declarations {
		if declaration.Validate() != nil {
			return PromptSubmitPlan{}, fmt.Errorf("invalid prompt submit declaration")
		}
		checks[index] = declaration.Reference
	}
	return buildPromptSubmitPlan(promptSubmitPlanFields{
		TurnID: assoc.TurnID(),
		TurnInputsPayload: TurnInputsPayload{
			Placement: assoc.Placement(), TargetSampleIndex: assoc.SampleIndex(), InputIDs: assoc.IDs(),
		},
		Checks: checks,
	})
}

func buildPromptSubmitPlan(fields promptSubmitPlanFields) (PromptSubmitPlan, error) {
	if _, err := domain.NewInputAssociation(fields.TurnID, fields.Placement, fields.TargetSampleIndex, fields.InputIDs); err != nil ||
		fields.TargetSampleIndex >= 16 || domain.ValidateUserPromptSubmitReferences(fields.Checks) != nil {
		return PromptSubmitPlan{}, fmt.Errorf("invalid prompt submit plan")
	}
	encoded, err := codec.MarshalStable(fields)
	if err != nil {
		return PromptSubmitPlan{}, err
	}
	return PromptSubmitPlan{fields: fields, digest: domain.HookDigest(encoded)}, nil
}

// Digest 返回不依赖正文的冻结计划摘要。
func (plan PromptSubmitPlan) Digest() string { return plan.digest }

// Target 为本次计划中的真实输入构造执行目标。
func (plan PromptSubmitPlan) Target(id domain.InputID) domain.PromptSubmitTarget {
	return domain.PromptSubmitTarget{TurnID: plan.fields.TurnID, InputID: id, SampleIndex: plan.fields.TargetSampleIndex}
}

// NewPromptSubmitPlannedDraft 记录待运行的真实 claim 及其检查。
func NewPromptSubmitPlannedDraft(plan PromptSubmitPlan) (RecordDraft, error) {
	if plan.digest == "" {
		return RecordDraft{}, fmt.Errorf("invalid prompt submit plan")
	}
	return encodeDraft(EventPromptSubmitHookPlanned, "", plan.fields.TurnID, promptSubmitPlanWire{plan.fields, plan.digest})
}

// DecodePromptSubmitPlanned 核对当前计划形状、关联与摘要。
func DecodePromptSubmitPlanned(record Record) (PromptSubmitPlan, error) {
	var wire promptSubmitPlanWire
	if record.EventKind != EventPromptSubmitHookPlanned || validatePromptSubmitEnvelope(record) != nil ||
		codec.DecodeObject(record.Payload, &wire, "turn_id", "placement", "target_sample_index", "input_ids", "checks", "digest") != nil {
		return PromptSubmitPlan{}, fmt.Errorf("invalid prompt submit plan encoding")
	}
	plan, err := buildPromptSubmitPlan(wire.promptSubmitPlanFields)
	if err != nil || plan.digest != wire.Digest || record.TurnID != plan.fields.TurnID {
		return PromptSubmitPlan{}, fmt.Errorf("invalid prompt submit plan evidence")
	}
	return plan, nil
}

// PromptSubmitDecision 记录一条真实输入对应的固定检查决定。
type PromptSubmitDecision struct {
	Target     domain.PromptSubmitTarget `json:"target"`
	PlanDigest string                    `json:"plan_digest"`
	Code       domain.HookCode           `json:"code"`
}

func (value PromptSubmitDecision) valid() bool {
	if value.Target.Validate() != nil || !domain.ValidHookDigest(value.PlanDigest) {
		return false
	}
	switch value.Code {
	case domain.HookOK:
		return true
	case domain.HookConfigInvalid, domain.HookMatchLimit, domain.HookOutcomeUncertain:
		return false
	default:
		return domain.ValidUserPromptSubmitRejection(value.Code)
	}
}

// NewPromptSubmitDecidedDraft 以可验证的计划身份记录通过或确定拒绝。
func NewPromptSubmitDecidedDraft(plan PromptSubmitPlan, inputID domain.InputID, code domain.HookCode) (RecordDraft, error) {
	value := PromptSubmitDecision{plan.Target(inputID), plan.digest, code}
	if !value.valid() || !slices.Contains(plan.fields.InputIDs, inputID) {
		return RecordDraft{}, fmt.Errorf("invalid prompt submit decision")
	}
	return encodeDraft(EventPromptSubmitHookDecided, "", plan.fields.TurnID, value)
}

// DecodePromptSubmitDecided 拒绝自由文本及非本事件决定。
func DecodePromptSubmitDecided(record Record) (PromptSubmitDecision, error) {
	value, err := decodeKnownPayload[PromptSubmitDecision](record, EventPromptSubmitHookDecided)
	if err != nil || validatePromptSubmitEnvelope(record) != nil || !value.valid() || value.Target.TurnID != record.TurnID {
		return PromptSubmitDecision{}, fmt.Errorf("invalid prompt submit decision")
	}
	return value, nil
}

// PromptSubmitCloseState 区分实时安全点与本地恢复的收口结果。
type PromptSubmitCloseState string

const (
	PromptSubmitCompleted   PromptSubmitCloseState = "completed"
	PromptSubmitCancelled   PromptSubmitCloseState = "cancelled"
	PromptSubmitUncertain   PromptSubmitCloseState = "outcome_uncertain"
	PromptSubmitInterrupted PromptSubmitCloseState = "interrupted"
)

type promptSubmitClosure struct {
	PlanDigest string                 `json:"plan_digest"`
	State      PromptSubmitCloseState `json:"state"`
}

func (value promptSubmitClosure) valid() bool {
	if !domain.ValidHookDigest(value.PlanDigest) {
		return false
	}
	switch value.State {
	case PromptSubmitCompleted, PromptSubmitCancelled, PromptSubmitUncertain, PromptSubmitInterrupted:
		return true
	default:
		return false
	}
}

// NewPromptSubmitClosedDraft 记录本次检查的收口状态，实际输入仍由独立批次提交。
func NewPromptSubmitClosedDraft(plan PromptSubmitPlan, state PromptSubmitCloseState) (RecordDraft, error) {
	value := promptSubmitClosure{plan.digest, state}
	if !value.valid() {
		return RecordDraft{}, fmt.Errorf("invalid prompt submit closure")
	}
	return encodeDraft(EventPromptSubmitHookClosed, "", plan.fields.TurnID, value)
}

func decodePromptSubmitClosed(record Record) (promptSubmitClosure, error) {
	value, err := decodeKnownPayload[promptSubmitClosure](record, EventPromptSubmitHookClosed)
	if err != nil || validatePromptSubmitEnvelope(record) != nil || !value.valid() {
		return promptSubmitClosure{}, fmt.Errorf("invalid prompt submit closure")
	}
	return value, nil
}

func validatePromptSubmitEnvelope(record Record) error {
	// Draft 的纯 payload 验证先于 writer 分配最终批次。
	if !record.TurnID.Valid() || record.ParentThreadID != "" ||
		record.BatchSize != 0 && (record.BatchSize != 1 || record.BatchID != record.Sequence || record.BatchIndex != 0) {
		return fmt.Errorf("invalid prompt submit record envelope")
	}
	return nil
}

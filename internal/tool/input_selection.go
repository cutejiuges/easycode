package tool

import (
	"errors"

	"easycode/internal/domain"
)

// ToolCallOrigin 绑定真实 ready 所属的会话及 sample/call 位置。
type ToolCallOrigin struct {
	SessionID   domain.SessionID
	ThreadID    domain.ThreadID
	TurnID      domain.TurnID
	SampleIndex uint32
	CallIndex   uint32
}

func NewToolCallOrigin(session domain.SessionID, thread domain.ThreadID, turn domain.TurnID, sample, call uint32) (ToolCallOrigin, error) {
	v := ToolCallOrigin{session, thread, turn, sample, call}
	return v, v.Validate()
}
func (v ToolCallOrigin) Validate() error {
	if !v.SessionID.Valid() || !v.ThreadID.Valid() || !v.TurnID.Valid() {
		return errors.New("invalid tool call origin")
	}
	return nil
}

// InputSelectionKind 不用 nil 或旧格式回退表达检查选择。
type InputSelectionKind string

const (
	InputUnhooked     InputSelectionKind = "unhooked"
	InputGateRejected InputSelectionKind = "gate_rejected"
	InputGateSelected InputSelectionKind = "gate_selected"
)

// InputSelection 只保存原始调用和选择证据；C/E 均由同一纯适配规则推导。
type InputSelection struct {
	kind     InputSelectionKind
	origin   ToolCallOrigin
	original Invocation
	plan     domain.HookPlan
	finished []domain.HookFinished
	decision domain.HookGateDecision
}

func NewUnhookedInputSelection(origin ToolCallOrigin, original Invocation) (InputSelection, error) {
	v := InputSelection{kind: InputUnhooked, origin: origin, original: original.Clone()}
	return v, v.Validate()
}
func NewRejectedInputSelection(origin ToolCallOrigin, original Invocation, plan domain.HookPlan, finished []domain.HookFinished, decision domain.HookGateDecision) (InputSelection, error) {
	return newGatedInputSelection(InputGateRejected, origin, original, plan, finished, decision)
}
func NewSelectedInputSelection(origin ToolCallOrigin, original Invocation, plan domain.HookPlan, finished []domain.HookFinished, decision domain.HookGateDecision) (InputSelection, error) {
	return newGatedInputSelection(InputGateSelected, origin, original, plan, finished, decision)
}
func newGatedInputSelection(kind InputSelectionKind, origin ToolCallOrigin, original Invocation, plan domain.HookPlan, finished []domain.HookFinished, decision domain.HookGateDecision) (InputSelection, error) {
	v := InputSelection{kind: kind, origin: origin, original: original.Clone(), plan: plan.Clone(), finished: clonePreFinished(finished), decision: decision.Clone()}
	return v, v.Validate()
}
func clonePreFinished(finished []domain.HookFinished) []domain.HookFinished {
	out := make([]domain.HookFinished, len(finished))
	for i, f := range finished {
		out[i] = f.Clone()
	}
	return out
}

// CheckingHookInput 验证已提交前缀并推导 C，不建立有效执行输入。
func CheckingHookInput(original Invocation, plan domain.HookPlan, finished []domain.HookFinished) (domain.HookToolInput, error) {
	input, err := HookInputForInvocation(original)
	profile, profileErr := HookProfileForInvocation(original)
	if err != nil || profileErr != nil || plan.Validate() != nil || plan.Event != domain.HookPreToolUse || plan.InvocationID != string(original.InvocationID()) || string(plan.Capability) != string(original.Capability()) || len(finished) > len(plan.Hooks) {
		return domain.HookToolInput{}, errors.New("invalid hook input prefix")
	}
	for i, f := range finished {
		id, _ := domain.NewHookRunIdentity(plan, i)
		if f.Validate() != nil || f.Result.Identity != id || f.Feedback.Reference != plan.Hooks[i] || i < len(finished)-1 && f.Result.Status != domain.HookContinue {
			return domain.HookToolInput{}, errors.New("invalid hook input prefix")
		}
		context, contextErr := domain.NewHookRewriteContext(input, profile)
		if contextErr != nil {
			return domain.HookToolInput{}, contextErr
		}
		input, err = f.InputEffect.ApplyAgainst(context)
		if err != nil {
			return domain.HookToolInput{}, err
		}
		if f.InputEffect.Kind() == domain.HookInputReplaced {
			if _, err := RewriteInvocation(original, input); err != nil {
				return domain.HookToolInput{}, err
			}
		}
	}
	return input, nil
}
func (v InputSelection) Validate() error {
	if v.origin.Validate() != nil || v.original.Validate() != nil {
		return errors.New("invalid input selection identity")
	}
	if v.kind == InputUnhooked {
		if v.plan.Digest != "" || len(v.finished) != 0 || v.decision.PlanDigest != "" {
			return errors.New("unhooked input contains gate facts")
		}
		return nil
	}
	if v.kind != InputGateRejected && v.kind != InputGateSelected {
		return errors.New("invalid input selection kind")
	}
	if _, err := CheckingHookInput(v.original, v.plan, v.finished); err != nil {
		return err
	}
	d := v.decision
	if d.Validate() != nil || d.PlanDigest != v.plan.Digest || d.InvocationID != string(v.original.InvocationID()) || d.Completed != len(v.finished) {
		return errors.New("invalid input gate selection")
	}
	if v.kind == InputGateSelected {
		if d.Status != domain.HookContinue || len(v.finished) != len(v.plan.Hooks) {
			return errors.New("incomplete selected gate")
		}
		for _, f := range v.finished {
			if f.Result.Status != domain.HookContinue {
				return errors.New("failed selected gate")
			}
		}
	} else {
		if d.Status == domain.HookContinue || d.BlockingOrdinal == nil {
			return errors.New("invalid rejected gate")
		}
		blocking := *d.BlockingOrdinal
		if blocking == len(v.finished) {
			if blocking >= len(v.plan.Hooks) {
				return errors.New("rejected gate has no unstarted suffix")
			}
			for _, f := range v.finished {
				if f.Result.Status != domain.HookContinue {
					return errors.New("invalid rejected prefix")
				}
			}
		} else if blocking+1 == len(v.finished) {
			last := v.finished[blocking].Result
			if last.Status != d.Status || last.Code != d.Code {
				return errors.New("rejected gate result mismatch")
			}
		} else {
			return errors.New("invalid rejected gate ordinal")
		}
	}
	return nil
}
func (v InputSelection) Kind() InputSelectionKind { return v.kind }
func (v InputSelection) Origin() ToolCallOrigin   { return v.origin }
func (v InputSelection) Original() Invocation     { return v.original.Clone() }
func (v InputSelection) Clone() InputSelection {
	v.original = v.original.Clone()
	v.plan = v.plan.Clone()
	v.finished = clonePreFinished(v.finished)
	v.decision = v.decision.Clone()
	return v
}
func (v InputSelection) Finished() []domain.HookFinished { return clonePreFinished(v.finished) }
func (v InputSelection) Plan() domain.HookPlan           { return v.plan.Clone() }

// Equal 比较同一真实调用的完整选择证明，不能只比较最终输入或计划摘要。
func (v InputSelection) Equal(other InputSelection) bool {
	if v.Validate() != nil || other.Validate() != nil || v.kind != other.kind || v.origin != other.origin || !v.original.Equal(other.original) || v.plan.Digest != other.plan.Digest || len(v.finished) != len(other.finished) {
		return false
	}
	a, b := v.decision, other.decision
	if a.InvocationID != b.InvocationID || a.PlanDigest != b.PlanDigest || a.Status != b.Status || a.Code != b.Code || a.Completed != b.Completed || (a.BlockingOrdinal == nil) != (b.BlockingOrdinal == nil) {
		return false
	}
	if a.BlockingOrdinal != nil && *a.BlockingOrdinal != *b.BlockingOrdinal {
		return false
	}
	for i, f := range v.finished {
		if !f.Equal(other.finished[i]) {
			return false
		}
	}
	return true
}

// ResultInvocation 在未选中候选时明确返回 O；普通执行只能消费 EffectiveInvocation。
func (v InputSelection) ResultInvocation() (Invocation, error) {
	if v.Validate() != nil {
		return Invocation{}, errors.New("invalid input selection")
	}
	if v.kind != InputGateSelected {
		return v.Original(), nil
	}
	input, err := CheckingHookInput(v.original, v.plan, v.finished)
	if err != nil {
		return Invocation{}, err
	}
	original, _ := HookInputForInvocation(v.original)
	if input.Equal(original) {
		return v.Original(), nil
	}
	return RewriteInvocation(v.original, input)
}
func (v InputSelection) EffectiveInvocation() (Invocation, bool) {
	if v.kind == InputGateRejected {
		return Invocation{}, false
	}
	inv, err := v.ResultInvocation()
	return inv, err == nil
}
func (v InputSelection) Changed() bool {
	if v.kind != InputGateSelected {
		return false
	}
	inv, ok := v.EffectiveInvocation()
	return ok && !inv.Equal(v.original)
}

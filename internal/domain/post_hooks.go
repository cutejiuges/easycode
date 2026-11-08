package domain

import (
	"easycode/internal/codec"
	"strconv"
)

// HookFeedbackDelivery 固定该调用是否有足够空间保存模型反馈。
type HookFeedbackDelivery string

const (
	HookDeliveryNone        HookFeedbackDelivery = "none"
	HookDeliveryAvailable   HookFeedbackDelivery = "available"
	HookDeliveryUnavailable HookFeedbackDelivery = "budget_unavailable"
)

// PostHookPlan 在执行前冻结引用与预算，不持有命令或授权凭据。
type PostHookPlan struct {
	Plan        HookPlan             `json:"plan"`
	Allowance   int                  `json:"allowance"`
	Reservation int                  `json:"reservation"`
	Delivery    HookFeedbackDelivery `json:"delivery"`
	Digest      string               `json:"digest"`
}

func NewPostHookPlan(plan HookPlan, allowance, reservation int, delivery HookFeedbackDelivery) (PostHookPlan, error) {
	v := PostHookPlan{Plan: plan.Clone(), Allowance: allowance, Reservation: reservation, Delivery: delivery}
	b, err := codec.MarshalStable(v)
	if err != nil {
		return v, err
	}
	v.Digest = HookDigest(b)
	return v, v.Validate()
}
func (v PostHookPlan) Validate() error {
	if v.Plan.Validate() != nil || v.Plan.Event != HookPostToolUse || v.Allowance < 1024 || v.Allowance > 256<<10 {
		return HookConfigInvalid
	}
	model := false
	for _, ref := range v.Plan.Hooks {
		model = model || ref.Feedback == HookFeedbackModel
	}
	reserved := 0
	if model {
		reserved = min(16<<10, v.Allowance-1024)
	}
	if v.Reservation != reserved || !model && v.Delivery != HookDeliveryNone || model && v.Delivery != HookDeliveryAvailable && v.Delivery != HookDeliveryUnavailable {
		return HookConfigInvalid
	}
	copy := v
	copy.Digest = ""
	b, err := codec.MarshalStable(copy)
	if err != nil || v.Digest != HookDigest(b) {
		return HookConfigInvalid
	}
	return nil
}
func (v PostHookPlan) Clone() PostHookPlan { v.Plan = v.Plan.Clone(); return v }
func DecodePostHookPlan(b []byte) (PostHookPlan, error) {
	var v PostHookPlan
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &v) != nil {
		return v, HookConfigInvalid
	}
	return v, v.Validate()
}

// PostHookDisposition 表达整个后置生命周期，不改变真实工具结果。
type PostHookDisposition string

const (
	PostHookCompleted PostHookDisposition = "completed"
	PostHookSkipped   PostHookDisposition = "skipped"
	PostHookCancelled PostHookDisposition = "cancelled"
	PostHookUncertain PostHookDisposition = "uncertain"
)

// PostHookBudget 保存调用最终额度证据，必须与原结果和反馈逐项交叉核验。
type PostHookBudget struct {
	Allowance      int `json:"allowance"`
	Reserved       int `json:"reserved"`
	BaseAllowance  int `json:"base_allowance"`
	BaseActual     int `json:"base_actual"`
	FeedbackActual int `json:"feedback_actual"`
	Released       int `json:"released"`
}

func NewPostHookBudget(allowance, reserved, baseActual, feedbackActual int) (PostHookBudget, error) {
	v := PostHookBudget{allowance, reserved, allowance - reserved, baseActual, feedbackActual, reserved - feedbackActual}
	return v, v.Validate()
}
func (v PostHookBudget) Validate() error {
	if v.Allowance < 1024 || v.Allowance > 256<<10 || v.Reserved < 0 || v.Reserved > 16<<10 || v.BaseAllowance != v.Allowance-v.Reserved || v.BaseAllowance < 1024 || v.BaseActual < 0 || v.BaseActual > v.BaseAllowance || v.FeedbackActual < 0 || v.FeedbackActual > v.Reserved || v.Released != v.Reserved-v.FeedbackActual {
		return HookConfigInvalid
	}
	return nil
}

// PostHookTerminal 只描述已运行前缀及未运行后缀，不能伪造运行统计。
type PostHookTerminal struct {
	InvocationID   string               `json:"invocation_id"`
	Event          HookEvent            `json:"event"`
	PlanDigest     string               `json:"plan_digest"`
	Origin         string               `json:"origin"`
	Disposition    PostHookDisposition  `json:"disposition"`
	Code           HookCode             `json:"code"`
	Processed      int                  `json:"processed"`
	UnstartedFrom  int                  `json:"unstarted_from"`
	SuffixFeedback []FrozenHookFeedback `json:"suffix_feedback"`
	Budget         PostHookBudget       `json:"budget"`
	FeedbackDigest string               `json:"feedback_digest"`
	Digest         string               `json:"digest"`
}

func NewPostHookTerminal(postPlan PostHookPlan, origin string, disposition PostHookDisposition, code HookCode, processed int, suffix []FrozenHookFeedback, budget PostHookBudget, feedbackDigest string) (PostHookTerminal, error) {
	plan := postPlan.Plan
	if postPlan.Validate() != nil || processed > len(plan.Hooks) {
		return PostHookTerminal{}, HookConfigInvalid
	}
	v := PostHookTerminal{InvocationID: plan.InvocationID, Event: HookPostToolUse, PlanDigest: postPlan.Digest, Origin: origin, Disposition: disposition, Code: code, Processed: processed, UnstartedFrom: processed, SuffixFeedback: append([]FrozenHookFeedback{}, suffix...), Budget: budget, FeedbackDigest: feedbackDigest}
	b, err := codec.MarshalStable(v)
	if err != nil {
		return v, err
	}
	v.Digest = HookDigest(b)
	return v, v.Validate()
}
func (v PostHookTerminal) Validate() error {
	if _, err := parseUUIDv7(v.InvocationID); err != nil {
		return HookConfigInvalid
	}
	if v.Event != HookPostToolUse || !ValidHookDigest(v.PlanDigest) || (v.Origin != "execution" && v.Origin != "recovery") || !v.Code.Valid() || v.Processed < 0 || v.Processed > MaxHookMatches || v.UnstartedFrom != v.Processed || v.SuffixFeedback == nil || len(v.SuffixFeedback) > MaxHookMatches-v.Processed || v.Budget.Validate() != nil || !ValidHookDigest(v.FeedbackDigest) {
		return HookConfigInvalid
	}
	switch v.Disposition {
	case PostHookCompleted:
		if v.Processed == 0 || len(v.SuffixFeedback) != 0 {
			return HookConfigInvalid
		}
	case PostHookSkipped:
		switch v.Code {
		case HookToolNotSuccessful, HookTrustDenied, HookTrustRequired, HookInputLimit, HookExecutionBudgetExhausted:
		default:
			return HookConfigInvalid
		}
	case PostHookCancelled:
		if v.Code != HookCancelled && v.Code != HookPostInterrupted {
			return HookConfigInvalid
		}
	case PostHookUncertain:
		if v.Code != HookOutcomeUncertain || v.Processed == 0 {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	if v.Origin == "recovery" && len(v.SuffixFeedback) != 0 {
		return HookConfigInvalid
	}
	last := v.Processed - 1
	for _, f := range v.SuffixFeedback {
		if f.Validate() != nil || f.Kind != HookFeedbackCode || f.Code != v.Code || f.Identity.InvocationID != v.InvocationID || f.Identity.PlanDigest != v.PlanDigest || f.Identity.Ordinal <= last {
			return HookConfigInvalid
		}
		last = f.Identity.Ordinal
	}
	copy := v
	copy.Digest = ""
	b, err := codec.MarshalStable(copy)
	if err != nil || v.Digest != HookDigest(b) {
		return HookConfigInvalid
	}
	return nil
}
func (v PostHookTerminal) Clone() PostHookTerminal {
	v.SuffixFeedback = append([]FrozenHookFeedback{}, v.SuffixFeedback...)
	return v
}
func DecodePostHookTerminal(b []byte) (PostHookTerminal, error) {
	var v PostHookTerminal
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &v) != nil {
		return v, HookConfigInvalid
	}
	return v, v.Validate()
}

// NewPostHookRunIdentity 同时绑定有序引用与已冻结预算计划。
func NewPostHookRunIdentity(p PostHookPlan, ordinal int) (HookRunIdentity, error) {
	if p.Validate() != nil {
		return HookRunIdentity{}, HookConfigInvalid
	}
	id, err := NewHookRunIdentity(p.Plan, ordinal)
	if err != nil {
		return HookRunIdentity{}, err
	}
	id.PlanDigest = p.Digest
	id.RunID = id.InvocationID + ":" + string(id.Event) + ":" + id.PlanDigest + ":" + strconv.Itoa(ordinal)
	return id, id.Validate()
}

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

// PostHookBranch 只保存一个可能事件的声明及披露状态，不复制调用归属。
type PostHookBranch struct {
	Event    HookEvent            `json:"event"`
	Hooks    []HookReference      `json:"hooks"`
	Delivery HookFeedbackDelivery `json:"delivery"`
}

func NewPostHookBranch(event HookEvent, refs []HookReference, reservation int) (PostHookBranch, error) {
	if reservation < 0 || reservation > 16<<10 {
		return PostHookBranch{}, HookConfigInvalid
	}
	v := PostHookBranch{Event: event, Hooks: append([]HookReference(nil), refs...), Delivery: HookDeliveryNone}
	models := 0
	for _, ref := range refs {
		if ref.Feedback == HookFeedbackModel {
			models++
		}
	}
	if models > 0 {
		v.Delivery = HookDeliveryUnavailable
		if reservation >= models*512+len("\n\n<post_tool_use_feedback>\n[]\n</post_tool_use_feedback>") {
			v.Delivery = HookDeliveryAvailable
		}
	}
	return v, v.Validate()
}

func (v PostHookBranch) Validate() error {
	if !v.Event.Post() || len(v.Hooks) == 0 || len(v.Hooks) > MaxHookMatches {
		return HookConfigInvalid
	}
	if err := validateHookReferences(v.Event, v.Hooks); err != nil {
		return err
	}
	model := false
	for _, ref := range v.Hooks {
		model = model || ref.Feedback == HookFeedbackModel
	}
	if !model && v.Delivery != HookDeliveryNone || model && v.Delivery != HookDeliveryAvailable && v.Delivery != HookDeliveryUnavailable {
		return HookConfigInvalid
	}
	return nil
}

func (v PostHookBranch) Clone() PostHookBranch {
	v.Hooks = append([]HookReference(nil), v.Hooks...)
	return v
}

func DecodePostHookBranch(b []byte) (PostHookBranch, error) {
	var v PostHookBranch
	if len(b) > MaxHookConfigBytes || codec.UnmarshalRecord(b, &v) != nil {
		return v, HookConfigInvalid
	}
	return v, v.Validate()
}

// PostHookPlan 在执行前冻结全部可能分支与唯一预算，不持有命令或授权凭据。
type PostHookPlan struct {
	InvocationID string           `json:"invocation_id"`
	Capability   HookCapability   `json:"capability"`
	Branches     []PostHookBranch `json:"branches"`
	Allowance    int              `json:"allowance"`
	Reservation  int              `json:"reservation"`
	Digest       string           `json:"digest"`
}

func NewPostHookPlan(invocation string, capability HookCapability, branches []PostHookBranch, allowance, reservation int) (PostHookPlan, error) {
	v := PostHookPlan{InvocationID: invocation, Capability: capability, Branches: branches, Allowance: allowance, Reservation: reservation}.Clone()
	b, err := codec.MarshalStable(v)
	if err != nil {
		return v, err
	}
	v.Digest = HookDigest(b)
	return v, v.Validate()
}
func (v PostHookPlan) Validate() error {
	if _, err := parseUUIDv7(v.InvocationID); err != nil {
		return HookConfigInvalid
	}
	if !v.Capability.Valid() || len(v.Branches) < 1 || len(v.Branches) > 2 || v.Allowance < 1024 || v.Allowance > 256<<10 {
		return HookConfigInvalid
	}
	model := false
	revisions := make(map[HookSource]string)
	seen := make(map[string]bool)
	for i, branch := range v.Branches {
		if branch.Validate() != nil || i > 0 && (v.Branches[i-1].Event != HookPostToolUse || branch.Event != HookPostToolUseFailure) {
			return HookConfigInvalid
		}
		expected, err := NewPostHookBranch(branch.Event, branch.Hooks, v.Reservation)
		if err != nil || branch.Delivery != expected.Delivery {
			return HookConfigInvalid
		}
		for _, ref := range branch.Hooks {
			key := string(ref.Source) + ":" + ref.ID
			if seen[key] || revisions[ref.Source] != "" && revisions[ref.Source] != ref.Revision {
				return HookConfigInvalid
			}
			seen[key] = true
			revisions[ref.Source] = ref.Revision
			model = model || ref.Feedback == HookFeedbackModel
		}
	}
	reserved := 0
	if model {
		reserved = min(16<<10, v.Allowance-1024)
	}
	if v.Reservation != reserved {
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
func (v PostHookPlan) Clone() PostHookPlan {
	v.Branches = append([]PostHookBranch(nil), v.Branches...)
	for i := range v.Branches {
		v.Branches[i] = v.Branches[i].Clone()
	}
	return v
}

// Branch 返回独立分支快照；未配置事件不制造空计划。
func (v PostHookPlan) Branch(event HookEvent) (PostHookBranch, bool) {
	for _, b := range v.Branches {
		if b.Event == event {
			return b.Clone(), true
		}
	}
	return PostHookBranch{}, false
}

// ModelFeedback 表示任一可能分支需要前置预留。
func (v PostHookPlan) ModelFeedback() bool {
	for _, b := range v.Branches {
		for _, ref := range b.Hooks {
			if ref.Feedback == HookFeedbackModel {
				return true
			}
		}
	}
	return false
}
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
	Route          PostHookRoute        `json:"route"`
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

func NewPostHookTerminal(postPlan PostHookPlan, route PostHookRoute, origin string, disposition PostHookDisposition, code HookCode, processed int, suffix []FrozenHookFeedback, budget PostHookBudget, feedbackDigest string) (PostHookTerminal, error) {
	event, _ := route.Event()
	branch, exists := postPlan.Branch(event)
	if postPlan.Validate() != nil || !route.Valid() || processed > len(branch.Hooks) || !exists && (processed != 0 || len(suffix) != 0) || budget.Allowance != postPlan.Allowance || budget.Reserved != postPlan.Reservation {
		return PostHookTerminal{}, HookConfigInvalid
	}
	if code == HookEventUnmatched && (exists || route == PostHookRouteNone) {
		return PostHookTerminal{}, HookConfigInvalid
	}
	v := PostHookTerminal{InvocationID: postPlan.InvocationID, Route: route, PlanDigest: postPlan.Digest, Origin: origin, Disposition: disposition, Code: code, Processed: processed, UnstartedFrom: processed, SuffixFeedback: append([]FrozenHookFeedback{}, suffix...), Budget: budget, FeedbackDigest: feedbackDigest}
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
	if !v.Route.Valid() || !ValidHookDigest(v.PlanDigest) || (v.Origin != "execution" && v.Origin != "recovery") || !v.Code.Valid() || v.Processed < 0 || v.Processed > MaxHookMatches || v.UnstartedFrom != v.Processed || v.SuffixFeedback == nil || len(v.SuffixFeedback) > MaxHookMatches-v.Processed || v.Budget.Validate() != nil || !ValidHookDigest(v.FeedbackDigest) {
		return HookConfigInvalid
	}
	if v.Route == PostHookRouteNone && (v.Processed != 0 || len(v.SuffixFeedback) != 0 || v.Disposition != PostHookSkipped && v.Disposition != PostHookCancelled) {
		return HookConfigInvalid
	}
	if v.Code == HookEventUnmatched && (v.Route == PostHookRouteNone || v.Disposition != PostHookSkipped || v.Processed != 0 || len(v.SuffixFeedback) != 0 || v.Budget.FeedbackActual != 0) {
		return HookConfigInvalid
	}
	if (v.Route == PostHookRouteNone || v.Code == HookEventUnmatched) && (v.Budget.FeedbackActual != 0 || v.FeedbackDigest != HookDigest(nil)) {
		return HookConfigInvalid
	}
	switch v.Disposition {
	case PostHookCompleted:
		if v.Processed == 0 || len(v.SuffixFeedback) != 0 {
			return HookConfigInvalid
		}
	case PostHookSkipped:
		switch v.Code {
		case HookToolNotSuccessful, HookTrustDenied, HookTrustRequired, HookInputLimit, HookExecutionBudgetExhausted, HookEventUnmatched:
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
		event, _ := v.Route.Event()
		if f.Validate() != nil || f.Kind != HookFeedbackCode || f.Code != v.Code || f.Identity.Event != event || f.Identity.InvocationID != v.InvocationID || f.Identity.PlanDigest != v.PlanDigest || f.Identity.Ordinal <= last {
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
func NewPostHookRunIdentity(p PostHookPlan, event HookEvent, ordinal int) (HookRunIdentity, error) {
	if p.Validate() != nil {
		return HookRunIdentity{}, HookConfigInvalid
	}
	branch, ok := p.Branch(event)
	if !ok || ordinal < 0 || ordinal >= len(branch.Hooks) {
		return HookRunIdentity{}, HookConfigInvalid
	}
	id := HookRunIdentity{InvocationID: p.InvocationID, Event: event, PlanDigest: p.Digest, Ordinal: ordinal}
	id.RunID = id.InvocationID + ":" + string(id.Event) + ":" + id.PlanDigest + ":" + strconv.Itoa(ordinal)
	return id, id.Validate()
}

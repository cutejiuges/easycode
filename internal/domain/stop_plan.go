package domain

import "easycode/internal/codec"

// StopPlanKind 区分完整检查列表与无法可靠选择声明的来源故障。
type StopPlanKind string

const (
	StopPlanChecks      StopPlanKind = "checks"
	StopPlanUnavailable StopPlanKind = "unavailable"
)

// StopPlan 冻结一次答复的完成门禁，不持有命令或信任状态。
type StopPlan struct {
	kind          StopPlanKind
	target        HookTarget
	continuations uint8
	checks        []HookReference
	code          HookCode
	digest        string
}
type stopPlanWire struct {
	Kind          StopPlanKind    `json:"kind"`
	Target        HookTarget      `json:"target"`
	Continuations uint8           `json:"continuations"`
	Checks        []HookReference `json:"checks,omitempty"`
	Code          HookCode        `json:"code,omitempty"`
	Digest        string          `json:"digest"`
}

func NewStopChecksPlan(sample AssistantSampleReference, continuations uint8, refs []HookReference) (StopPlan, error) {
	target, err := NewStopHookTarget(sample)
	if err != nil {
		return StopPlan{}, err
	}
	v := StopPlan{kind: StopPlanChecks, target: target, continuations: continuations, checks: append([]HookReference(nil), refs...)}
	return freezeStopPlan(v)
}
func NewUnavailableStopPlan(sample AssistantSampleReference, continuations uint8, code HookCode) (StopPlan, error) {
	target, err := NewStopHookTarget(sample)
	if err != nil {
		return StopPlan{}, err
	}
	return freezeStopPlan(StopPlan{kind: StopPlanUnavailable, target: target, continuations: continuations, code: code})
}
func freezeStopPlan(v StopPlan) (StopPlan, error) {
	b, err := codec.MarshalStable(v.wire())
	if err != nil {
		return StopPlan{}, HookConfigInvalid
	}
	v.digest = HookDigest(b)
	return v, v.Validate()
}
func (v StopPlan) wire() stopPlanWire {
	return stopPlanWire{v.kind, v.target, v.continuations, append([]HookReference(nil), v.checks...), v.code, v.digest}
}
func (v StopPlan) Kind() StopPlanKind               { return v.kind }
func (v StopPlan) Target() HookTarget               { return v.target }
func (v StopPlan) Sample() AssistantSampleReference { s, _ := v.target.AssistantSample(); return s }
func (v StopPlan) Continuations() uint8             { return v.continuations }
func (v StopPlan) Checks() []HookReference          { return append([]HookReference(nil), v.checks...) }
func (v StopPlan) UnavailableCode() HookCode        { return v.code }
func (v StopPlan) Digest() string                   { return v.digest }
func (v StopPlan) Clone() StopPlan                  { v.checks = v.Checks(); return v }
func (v StopPlan) Validate() error {
	if !v.target.ValidFor(HookStop) || v.continuations > MaxStopContinuations {
		return HookConfigInvalid
	}
	switch v.kind {
	case StopPlanChecks:
		if len(v.checks) < 1 || len(v.checks) > MaxHookMatches || v.code != "" || validateHookReferences(HookStop, v.checks) != nil {
			return HookConfigInvalid
		}
	case StopPlanUnavailable:
		if len(v.checks) != 0 || v.code != HookConfigInvalid && v.code != HookMatchLimit {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	w := v.wire()
	w.Digest = ""
	b, err := codec.MarshalStable(w)
	if err != nil || v.digest != HookDigest(b) {
		return HookConfigInvalid
	}
	return nil
}
func (v StopPlan) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, HookConfigInvalid
	}
	return codec.MarshalStable(v.wire())
}
func DecodeStopPlan(b []byte) (StopPlan, error) {
	var w stopPlanWire
	if len(b) > MaxHookConfigBytes || codec.DecodeObject(b, &w, "kind", "target", "continuations", "digest") != nil {
		return StopPlan{}, HookConfigInvalid
	}
	m, err := codec.ObjectMembers(b)
	if err != nil {
		return StopPlan{}, HookConfigInvalid
	}
	_, checks := m["checks"]
	_, code := m["code"]
	if checks != (w.Kind == StopPlanChecks) || code != (w.Kind == StopPlanUnavailable) {
		return StopPlan{}, HookConfigInvalid
	}
	v := StopPlan{w.Kind, w.Target, w.Continuations, append([]HookReference(nil), w.Checks...), w.Code, w.Digest}
	return v, v.Validate()
}
func (v *StopPlan) UnmarshalJSON(b []byte) error {
	next, err := DecodeStopPlan(b)
	if err != nil {
		return err
	}
	*v = next
	return nil
}

func NewStopHookRunIdentity(p StopPlan, ordinal int) (HookRunIdentity, error) {
	if p.Validate() != nil || p.kind != StopPlanChecks || ordinal < 0 || ordinal >= len(p.checks) {
		return HookRunIdentity{}, HookConfigInvalid
	}
	return newHookRunIdentity(p.target, HookStop, p.digest, ordinal)
}

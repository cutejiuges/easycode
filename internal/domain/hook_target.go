package domain

import (
	"strconv"

	"easycode/internal/codec"
)

// HookTargetKind 区分真实工具调用与已提交的回合答复。
type HookTargetKind string

const (
	HookTargetToolCall HookTargetKind = "tool_call"
	HookTargetTurnStop HookTargetKind = "turn_stop"
)

// AssistantSampleReference 精确定位一个回合中的成功采样，不使用展示位置。
type AssistantSampleReference struct {
	TurnID      TurnID `json:"turn_id"`
	SampleIndex uint32 `json:"sample_index"`
}

func NewAssistantSampleReference(turn TurnID, index uint32) (AssistantSampleReference, error) {
	v := AssistantSampleReference{turn, index}
	return v, v.Validate()
}
func (v AssistantSampleReference) Validate() error {
	if !v.TurnID.Valid() || v.SampleIndex >= 16 {
		return HookConfigInvalid
	}
	return nil
}

// HookTarget 是只有两个当前分支的值联合；构造和编解码均不执行副作用。
type HookTarget struct {
	kind       HookTargetKind
	invocation string
	capability HookCapability
	sample     AssistantSampleReference
}

func NewToolHookTarget(invocation string, capability HookCapability) (HookTarget, error) {
	v := HookTarget{kind: HookTargetToolCall, invocation: invocation, capability: capability}
	return v, v.Validate()
}
func NewStopHookTarget(sample AssistantSampleReference) (HookTarget, error) {
	v := HookTarget{kind: HookTargetTurnStop, sample: sample}
	return v, v.Validate()
}
func (v HookTarget) Kind() HookTargetKind { return v.kind }
func (v HookTarget) ToolCall() (string, HookCapability, bool) {
	return v.invocation, v.capability, v.kind == HookTargetToolCall && v.Validate() == nil
}
func (v HookTarget) AssistantSample() (AssistantSampleReference, bool) {
	return v.sample, v.kind == HookTargetTurnStop && v.Validate() == nil
}
func (v HookTarget) Validate() error {
	switch v.kind {
	case HookTargetToolCall:
		if _, err := parseUUIDv7(v.invocation); err != nil || !v.capability.Valid() || v.sample != (AssistantSampleReference{}) {
			return HookConfigInvalid
		}
	case HookTargetTurnStop:
		if v.sample.Validate() != nil || v.invocation != "" || v.capability != "" {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	return nil
}
func (v HookTarget) ValidFor(event HookEvent) bool {
	return v.Validate() == nil && event.Valid() && (event == HookStop) == (v.kind == HookTargetTurnStop)
}
func (v HookTarget) Key() string {
	switch v.kind {
	case HookTargetToolCall:
		return string(v.kind) + ":" + v.invocation + ":" + string(v.capability)
	case HookTargetTurnStop:
		return string(v.kind) + ":" + string(v.sample.TurnID) + ":" + strconv.FormatUint(uint64(v.sample.SampleIndex), 10)
	default:
		return ""
	}
}
func (v HookTarget) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, HookConfigInvalid
	}
	if v.kind == HookTargetToolCall {
		return codec.MarshalStable(struct {
			Kind       HookTargetKind `json:"kind"`
			Invocation string         `json:"invocation_id"`
			Capability HookCapability `json:"capability"`
		}{v.kind, v.invocation, v.capability})
	}
	return codec.MarshalStable(struct {
		Kind   HookTargetKind `json:"kind"`
		TurnID TurnID         `json:"turn_id"`
		Index  uint32         `json:"sample_index"`
	}{v.kind, v.sample.TurnID, v.sample.SampleIndex})
}
func DecodeHookTarget(b []byte) (HookTarget, error) {
	m, err := codec.ObjectMembers(b)
	if err != nil {
		return HookTarget{}, HookConfigInvalid
	}
	var kind HookTargetKind
	if codec.UnmarshalRecord(m["kind"], &kind) != nil {
		return HookTarget{}, HookConfigInvalid
	}
	switch kind {
	case HookTargetToolCall:
		var w struct {
			Kind       HookTargetKind `json:"kind"`
			Invocation string         `json:"invocation_id"`
			Capability HookCapability `json:"capability"`
		}
		if codec.DecodeObject(b, &w, "kind", "invocation_id", "capability") != nil {
			return HookTarget{}, HookConfigInvalid
		}
		return NewToolHookTarget(w.Invocation, w.Capability)
	case HookTargetTurnStop:
		var w struct {
			Kind   HookTargetKind `json:"kind"`
			TurnID TurnID         `json:"turn_id"`
			Index  uint32         `json:"sample_index"`
		}
		if codec.DecodeObject(b, &w, "kind", "turn_id", "sample_index") != nil {
			return HookTarget{}, HookConfigInvalid
		}
		return NewStopHookTarget(AssistantSampleReference{w.TurnID, w.Index})
	default:
		return HookTarget{}, HookConfigInvalid
	}
}
func (v *HookTarget) UnmarshalJSON(b []byte) error {
	next, err := DecodeHookTarget(b)
	if err != nil {
		return err
	}
	*v = next
	return nil
}

// HookScope 将工具 matcher 与无 matcher 的完成检查声明隔离。
type HookScope struct {
	kind    HookTargetKind
	matcher HookMatcher
}

func NewToolHookScope(matcher HookMatcher) (HookScope, error) {
	v := HookScope{kind: HookTargetToolCall, matcher: matcher}
	return v, v.Validate()
}
func NewStopHookScope() HookScope { return HookScope{kind: HookTargetTurnStop} }
func (v HookScope) Validate() error {
	if v.kind == HookTargetToolCall && v.matcher.Valid() || v.kind == HookTargetTurnStop && v.matcher == 0 {
		return nil
	}
	return HookConfigInvalid
}
func (v HookScope) ValidFor(event HookEvent) bool {
	return v.Validate() == nil && event.Valid() && (event == HookStop) == (v.kind == HookTargetTurnStop)
}
func (v HookScope) ToolMatcher() (HookMatcher, bool) {
	return v.matcher, v.kind == HookTargetToolCall && v.Validate() == nil
}
func (v HookScope) Matches(cap HookCapability) bool {
	return v.kind == HookTargetToolCall && v.matcher.Matches(cap)
}
func (v HookScope) String() string {
	if v.kind == HookTargetTurnStop {
		return "Turn completion"
	}
	return v.matcher.String()
}
func (v HookScope) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, HookConfigInvalid
	}
	if v.kind == HookTargetTurnStop {
		return codec.MarshalStable(struct {
			Kind HookTargetKind `json:"kind"`
		}{v.kind})
	}
	return codec.MarshalStable(struct {
		Kind    HookTargetKind `json:"kind"`
		Matcher HookMatcher    `json:"matcher"`
	}{v.kind, v.matcher})
}
func DecodeHookScope(b []byte) (HookScope, error) {
	m, err := codec.ObjectMembers(b)
	if err != nil {
		return HookScope{}, HookConfigInvalid
	}
	var kind HookTargetKind
	if codec.UnmarshalRecord(m["kind"], &kind) != nil {
		return HookScope{}, HookConfigInvalid
	}
	if kind == HookTargetTurnStop {
		var w struct {
			Kind HookTargetKind `json:"kind"`
		}
		if codec.DecodeObject(b, &w, "kind") != nil {
			return HookScope{}, HookConfigInvalid
		}
		return NewStopHookScope(), nil
	}
	var w struct {
		Kind    HookTargetKind `json:"kind"`
		Matcher HookMatcher    `json:"matcher"`
	}
	if kind != HookTargetToolCall || codec.DecodeObject(b, &w, "kind", "matcher") != nil {
		return HookScope{}, HookConfigInvalid
	}
	return NewToolHookScope(w.Matcher)
}
func (v *HookScope) UnmarshalJSON(b []byte) error {
	next, err := DecodeHookScope(b)
	if err != nil {
		return err
	}
	*v = next
	return nil
}

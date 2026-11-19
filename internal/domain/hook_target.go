package domain

import (
	"strconv"

	"easycode/internal/codec"
)

// HookTargetKind 区分真实工具调用、回合答复与压缩操作。
type HookTargetKind string

const (
	HookTargetToolCall   HookTargetKind = "tool_call"
	HookTargetTurnStop   HookTargetKind = "turn_stop"
	HookTargetCompaction HookTargetKind = "compaction"
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

// HookTarget 是当前检查目标的值联合；构造和编解码均不执行副作用。
type HookTarget struct {
	kind       HookTargetKind
	invocation string
	capability HookCapability
	sample     AssistantSampleReference
	compaction CompactionID
	scope      CompactionScope
}

func NewToolHookTarget(invocation string, capability HookCapability) (HookTarget, error) {
	v := HookTarget{kind: HookTargetToolCall, invocation: invocation, capability: capability}
	return v, v.Validate()
}
func NewStopHookTarget(sample AssistantSampleReference) (HookTarget, error) {
	v := HookTarget{kind: HookTargetTurnStop, sample: sample}
	return v, v.Validate()
}

// NewPreCompactHookTarget 绑定真实压缩身份及触发作用域。
func NewPreCompactHookTarget(id CompactionID, scope CompactionScope) (HookTarget, error) {
	v := HookTarget{kind: HookTargetCompaction, compaction: id, scope: scope}
	return v, v.Validate()
}

func (v HookTarget) Kind() HookTargetKind { return v.kind }
func (v HookTarget) ToolCall() (string, HookCapability, bool) {
	return v.invocation, v.capability, v.kind == HookTargetToolCall && v.Validate() == nil
}
func (v HookTarget) AssistantSample() (AssistantSampleReference, bool) {
	return v.sample, v.kind == HookTargetTurnStop && v.Validate() == nil
}

func (v HookTarget) Compaction() (CompactionID, CompactionScope, bool) {
	return v.compaction, v.scope, v.kind == HookTargetCompaction && v.Validate() == nil
}

func (v HookTarget) Validate() error {
	switch v.kind {
	case HookTargetToolCall:
		if _, err := parseUUIDv7(v.invocation); err != nil || !v.capability.Valid() || v.sample != (AssistantSampleReference{}) || v.compaction != "" || v.scope != (CompactionScope{}) {
			return HookConfigInvalid
		}
	case HookTargetTurnStop:
		if v.sample.Validate() != nil || v.invocation != "" || v.capability != "" || v.compaction != "" || v.scope != (CompactionScope{}) {
			return HookConfigInvalid
		}
	case HookTargetCompaction:
		if !v.compaction.Valid() || v.scope.Validate() != nil || v.invocation != "" || v.capability != "" || v.sample != (AssistantSampleReference{}) {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	return nil
}
func (v HookTarget) ValidFor(event HookEvent) bool {
	return v.kind == event.targetKind() && v.Validate() == nil
}
func (v HookTarget) Key() string {
	switch v.kind {
	case HookTargetToolCall:
		return string(v.kind) + ":" + v.invocation + ":" + string(v.capability)
	case HookTargetTurnStop:
		return string(v.kind) + ":" + string(v.sample.TurnID) + ":" + strconv.FormatUint(uint64(v.sample.SampleIndex), 10)
	case HookTargetCompaction:
		return string(v.kind) + ":" + string(v.compaction) + ":" + string(v.scope.Trigger) + ":" + string(v.scope.TurnID) + ":" + strconv.FormatUint(uint64(v.scope.SampleIndex), 10)
	default:
		return ""
	}
}
func (v HookTarget) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, HookConfigInvalid
	}
	switch v.kind {
	case HookTargetToolCall:
		return codec.MarshalStable(struct {
			Kind       HookTargetKind `json:"kind"`
			Invocation string         `json:"invocation_id"`
			Capability HookCapability `json:"capability"`
		}{v.kind, v.invocation, v.capability})
	case HookTargetTurnStop:
		return codec.MarshalStable(struct {
			Kind   HookTargetKind `json:"kind"`
			TurnID TurnID         `json:"turn_id"`
			Index  uint32         `json:"sample_index"`
		}{v.kind, v.sample.TurnID, v.sample.SampleIndex})
	default:
		return codec.MarshalStable(compactionHookTargetWire{v.kind, v.compaction, v.scope})
	}
}

type compactionHookTargetWire struct {
	Kind  HookTargetKind  `json:"kind"`
	ID    CompactionID    `json:"compaction_id"`
	Scope CompactionScope `json:"scope"`
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
	case HookTargetCompaction:
		var w compactionHookTargetWire
		if codec.UnmarshalRecord(b, &w) != nil {
			return HookTarget{}, HookConfigInvalid
		}
		return NewPreCompactHookTarget(w.ID, w.Scope)
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

const anyCompactionTrigger CompactionTrigger = "*"

// HookScope 隔离工具 matcher、无 matcher 的回合检查和压缩触发 matcher。
type HookScope struct {
	kind              HookTargetKind
	matcher           HookMatcher
	compactionMatcher CompactionTrigger
}

func NewToolHookScope(matcher HookMatcher) (HookScope, error) {
	v := HookScope{kind: HookTargetToolCall, matcher: matcher}
	return v, v.Validate()
}
func NewStopHookScope() HookScope { return HookScope{kind: HookTargetTurnStop} }

// ParsePreCompactHookScope 只接受当前三种规范触发 matcher。
func ParsePreCompactHookScope(matcher string) (HookScope, error) {
	v := HookScope{kind: HookTargetCompaction, compactionMatcher: CompactionTrigger(matcher)}
	return v, v.Validate()
}

func (v HookScope) Validate() error {
	switch {
	case v.kind == HookTargetToolCall && v.matcher.Valid() && v.compactionMatcher == "":
		return nil
	case v.kind == HookTargetTurnStop && v.matcher == 0 && v.compactionMatcher == "":
		return nil
	case v.kind == HookTargetCompaction && v.matcher == 0 && (v.compactionMatcher == CompactionManual || v.compactionMatcher == CompactionAuto || v.compactionMatcher == anyCompactionTrigger):
		return nil
	}
	return HookConfigInvalid
}
func (v HookScope) ValidFor(event HookEvent) bool {
	return v.kind == event.targetKind() && v.Validate() == nil
}
func (v HookScope) ToolMatcher() (HookMatcher, bool) {
	return v.matcher, v.kind == HookTargetToolCall && v.Validate() == nil
}
func (v HookScope) Matches(cap HookCapability) bool {
	return v.kind == HookTargetToolCall && v.matcher.Matches(cap)
}

func (v HookScope) CompactionMatcher() (string, bool) {
	return string(v.compactionMatcher), v.kind == HookTargetCompaction && v.Validate() == nil
}

func (v HookScope) MatchesCompaction(trigger CompactionTrigger) bool {
	return v.kind == HookTargetCompaction && (trigger == CompactionManual || trigger == CompactionAuto) && (v.compactionMatcher == trigger || v.compactionMatcher == anyCompactionTrigger)
}

func (v HookScope) String() string {
	switch v.kind {
	case HookTargetTurnStop:
		return "Turn completion"
	case HookTargetCompaction:
		return string(v.compactionMatcher)
	default:
		return v.matcher.String()
	}
}
func (v HookScope) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, HookConfigInvalid
	}
	switch v.kind {
	case HookTargetTurnStop:
		return codec.MarshalStable(struct {
			Kind HookTargetKind `json:"kind"`
		}{v.kind})
	case HookTargetCompaction:
		return codec.MarshalStable(compactionHookScopeWire{v.kind, string(v.compactionMatcher)})
	default:
		return codec.MarshalStable(struct {
			Kind    HookTargetKind `json:"kind"`
			Matcher HookMatcher    `json:"matcher"`
		}{v.kind, v.matcher})
	}
}

type compactionHookScopeWire struct {
	Kind    HookTargetKind `json:"kind"`
	Matcher string         `json:"matcher"`
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
	switch kind {
	case HookTargetTurnStop:
		var w struct {
			Kind HookTargetKind `json:"kind"`
		}
		if codec.DecodeObject(b, &w, "kind") != nil {
			return HookScope{}, HookConfigInvalid
		}
		return NewStopHookScope(), nil
	case HookTargetCompaction:
		var w compactionHookScopeWire
		if codec.DecodeObject(b, &w, "kind", "matcher") != nil {
			return HookScope{}, HookConfigInvalid
		}
		return ParsePreCompactHookScope(w.Matcher)
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

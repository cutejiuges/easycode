package domain

import (
	"strconv"

	"easycode/internal/codec"
)

// HookTargetKind 区分真实输入、工具调用、回合答复和会话操作。
type HookTargetKind string

const (
	HookTargetToolCall         HookTargetKind = "tool_call"
	HookTargetTurnStop         HookTargetKind = "turn_stop"
	HookTargetCompaction       HookTargetKind = "compaction"
	HookTargetSessionStart     HookTargetKind = "session_start"
	HookTargetUserPromptSubmit HookTargetKind = "user_prompt_submit"
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
	kind         HookTargetKind
	invocation   string
	capability   HookCapability
	sample       AssistantSampleReference
	compaction   CompactionID
	scope        CompactionScope
	sessionStart SessionStartTarget
	promptSubmit PromptSubmitTarget
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
	return NewCompactionHookTarget(id, scope)
}

// NewCompactionHookTarget 绑定两类压缩检查共用的真实操作目标。
func NewCompactionHookTarget(id CompactionID, scope CompactionScope) (HookTarget, error) {
	v := HookTarget{kind: HookTargetCompaction, compaction: id, scope: scope}
	return v, v.Validate()
}

// NewSessionStartHookTarget 绑定真实会话激活或已安装压缩检查点。
func NewSessionStartHookTarget(target SessionStartTarget) (HookTarget, error) {
	v := HookTarget{kind: HookTargetSessionStart, sessionStart: target}
	return v, v.Validate()
}

// NewUserPromptSubmitHookTarget 绑定真实输入和领取它的采样位置。
func NewUserPromptSubmitHookTarget(target PromptSubmitTarget) (HookTarget, error) {
	v := HookTarget{kind: HookTargetUserPromptSubmit, promptSubmit: target}
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
	if v.kind == HookTargetSessionStart && v.sessionStart.Source == SessionStartCompact {
		return v.sessionStart.CompactionID, v.sessionStart.Scope, v.Validate() == nil
	}
	return v.compaction, v.scope, v.kind == HookTargetCompaction && v.Validate() == nil
}

// SessionStart 读取与 SessionStart 声明匹配的目标身份。
func (v HookTarget) SessionStart() (SessionStartTarget, bool) {
	return v.sessionStart, v.kind == HookTargetSessionStart && v.Validate() == nil
}

// UserPromptSubmit 读取当前输入检查的目标身份。
func (v HookTarget) UserPromptSubmit() (PromptSubmitTarget, bool) {
	return v.promptSubmit, v.kind == HookTargetUserPromptSubmit && v.Validate() == nil
}

func (v HookTarget) Validate() error {
	if v.kind != HookTargetSessionStart && v.sessionStart != (SessionStartTarget{}) ||
		v.kind != HookTargetUserPromptSubmit && v.promptSubmit != (PromptSubmitTarget{}) {
		return HookConfigInvalid
	}
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
	case HookTargetSessionStart:
		if v.sessionStart.Validate() != nil || v.invocation != "" || v.capability != "" || v.sample != (AssistantSampleReference{}) ||
			v.compaction != "" || v.scope != (CompactionScope{}) {
			return HookConfigInvalid
		}
	case HookTargetUserPromptSubmit:
		if v.promptSubmit.Validate() != nil || v.invocation != "" || v.capability != "" || v.sample != (AssistantSampleReference{}) ||
			v.compaction != "" || v.scope != (CompactionScope{}) {
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
	case HookTargetSessionStart:
		return string(v.kind) + ":" + v.sessionStart.key()
	case HookTargetUserPromptSubmit:
		return string(v.kind) + ":" + string(v.promptSubmit.TurnID) + ":" + string(v.promptSubmit.InputID) + ":" + strconv.FormatUint(uint64(v.promptSubmit.SampleIndex), 10)
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
	case HookTargetSessionStart:
		return codec.MarshalStable(sessionStartHookTargetWire{v.kind, v.sessionStart})
	case HookTargetUserPromptSubmit:
		return codec.MarshalStable(promptSubmitHookTargetWire{v.kind, v.promptSubmit})
	default:
		return codec.MarshalStable(compactionHookTargetWire{v.kind, v.compaction, v.scope})
	}
}

type compactionHookTargetWire struct {
	Kind  HookTargetKind  `json:"kind"`
	ID    CompactionID    `json:"compaction_id"`
	Scope CompactionScope `json:"scope"`
}

type sessionStartHookTargetWire struct {
	Kind   HookTargetKind     `json:"kind"`
	Target SessionStartTarget `json:"session_start"`
}

type promptSubmitHookTargetWire struct {
	Kind HookTargetKind `json:"kind"`
	PromptSubmitTarget
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
		return NewCompactionHookTarget(w.ID, w.Scope)
	case HookTargetSessionStart:
		var w sessionStartHookTargetWire
		if codec.UnmarshalRecord(b, &w) != nil {
			return HookTarget{}, HookConfigInvalid
		}
		return NewSessionStartHookTarget(w.Target)
	case HookTargetUserPromptSubmit:
		var w promptSubmitHookTargetWire
		if codec.DecodeObject(b, &w, "kind", "turn_id", "input_id", "sample_index") != nil {
			return HookTarget{}, HookConfigInvalid
		}
		return NewUserPromptSubmitHookTarget(w.PromptSubmitTarget)
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

// HookScope 隔离无条件事件、工具能力、压缩触发和会话来源。
type HookScope struct {
	kind                HookTargetKind
	matcher             HookMatcher
	compactionMatcher   CompactionTrigger
	sessionStartMatcher SessionStartSource
}

func NewToolHookScope(matcher HookMatcher) (HookScope, error) {
	v := HookScope{kind: HookTargetToolCall, matcher: matcher}
	return v, v.Validate()
}
func NewStopHookScope() HookScope { return HookScope{kind: HookTargetTurnStop} }

// NewUserPromptSubmitHookScope 构造不按输入内容匹配的检查范围。
func NewUserPromptSubmitHookScope() HookScope { return HookScope{kind: HookTargetUserPromptSubmit} }

// ParsePreCompactHookScope 只接受当前三种规范触发 matcher。
func ParsePreCompactHookScope(matcher string) (HookScope, error) {
	return ParseCompactionHookScope(matcher)
}

// ParseCompactionHookScope 读取两类压缩检查共用的字面触发 matcher。
func ParseCompactionHookScope(matcher string) (HookScope, error) {
	v := HookScope{kind: HookTargetCompaction, compactionMatcher: CompactionTrigger(matcher)}
	return v, v.Validate()
}

// ParseSessionStartHookScope 只接受当前来源字面值和通配符。
func ParseSessionStartHookScope(matcher string) (HookScope, error) {
	v := HookScope{kind: HookTargetSessionStart, sessionStartMatcher: SessionStartSource(matcher)}
	return v, v.Validate()
}

func (v HookScope) Validate() error {
	if v.kind != HookTargetSessionStart && v.sessionStartMatcher != "" {
		return HookConfigInvalid
	}
	switch {
	case v.kind == HookTargetToolCall && v.matcher.Valid() && v.compactionMatcher == "":
		return nil
	case (v.kind == HookTargetTurnStop || v.kind == HookTargetUserPromptSubmit) && v.matcher == 0 && v.compactionMatcher == "":
		return nil
	case v.kind == HookTargetCompaction && v.matcher == 0 && (v.compactionMatcher == CompactionManual || v.compactionMatcher == CompactionAuto || v.compactionMatcher == anyCompactionTrigger):
		return nil
	case v.kind == HookTargetSessionStart && v.matcher == 0 && v.compactionMatcher == "" &&
		(v.sessionStartMatcher.Valid() || v.sessionStartMatcher == anySessionStartSource):
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

// SessionStartMatcher 返回声明中配置的会话来源或通配符。
func (v HookScope) SessionStartMatcher() (string, bool) {
	return string(v.sessionStartMatcher), v.kind == HookTargetSessionStart && v.Validate() == nil
}

// MatchesSessionStart 按声明匹配实际会话来源。
func (v HookScope) MatchesSessionStart(source SessionStartSource) bool {
	return v.kind == HookTargetSessionStart && source.Valid() && (v.sessionStartMatcher == source || v.sessionStartMatcher == anySessionStartSource)
}

func (v HookScope) String() string {
	switch v.kind {
	case HookTargetTurnStop:
		return "Turn completion"
	case HookTargetUserPromptSubmit:
		return "Prompt submission"
	case HookTargetCompaction:
		return string(v.compactionMatcher)
	case HookTargetSessionStart:
		return string(v.sessionStartMatcher)
	default:
		return v.matcher.String()
	}
}
func (v HookScope) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, HookConfigInvalid
	}
	switch v.kind {
	case HookTargetTurnStop, HookTargetUserPromptSubmit:
		return codec.MarshalStable(struct {
			Kind HookTargetKind `json:"kind"`
		}{v.kind})
	case HookTargetCompaction:
		return codec.MarshalStable(compactionHookScopeWire{v.kind, string(v.compactionMatcher)})
	case HookTargetSessionStart:
		return codec.MarshalStable(compactionHookScopeWire{v.kind, string(v.sessionStartMatcher)})
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
	case HookTargetTurnStop, HookTargetUserPromptSubmit:
		var w struct {
			Kind HookTargetKind `json:"kind"`
		}
		if codec.DecodeObject(b, &w, "kind") != nil {
			return HookScope{}, HookConfigInvalid
		}
		return HookScope{kind: kind}, nil
	case HookTargetCompaction:
		var w compactionHookScopeWire
		if codec.DecodeObject(b, &w, "kind", "matcher") != nil {
			return HookScope{}, HookConfigInvalid
		}
		return ParseCompactionHookScope(w.Matcher)
	case HookTargetSessionStart:
		var w compactionHookScopeWire
		if codec.DecodeObject(b, &w, "kind", "matcher") != nil {
			return HookScope{}, HookConfigInvalid
		}
		return ParseSessionStartHookScope(w.Matcher)
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

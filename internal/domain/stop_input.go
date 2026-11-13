package domain

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

// StopInput 只向受信任本地命令提供指定答复，不持有工具或原生推理数据。
type StopInput struct {
	session SessionID
	thread  ThreadID
	sample  AssistantSampleReference
	cwd     string
	active  bool
	message string
}

type stopInputWire struct {
	Event   HookEvent `json:"hook_event_name"`
	Session SessionID `json:"session_id"`
	Thread  ThreadID  `json:"thread_id"`
	Turn    TurnID    `json:"turn_id"`
	Index   uint32    `json:"sample_index"`
	CWD     string    `json:"cwd"`
	Active  bool      `json:"stop_hook_active"`
	Message string    `json:"last_assistant_message"`
}

func NewStopInput(session SessionID, thread ThreadID, sample AssistantSampleReference, cwd string, active bool, message string) (StopInput, error) {
	v := StopInput{session, thread, sample, cwd, active, message}
	return v, v.Validate()
}
func (v StopInput) wire() stopInputWire {
	return stopInputWire{HookStop, v.session, v.thread, v.sample.TurnID, v.sample.SampleIndex, v.cwd, v.active, v.message}
}
func (v StopInput) Validate() error {
	if !v.session.Valid() || !v.thread.Valid() || v.sample.Validate() != nil || !filepath.IsAbs(v.cwd) || !utf8.ValidString(v.cwd) || strings.ContainsRune(v.cwd, 0) || !utf8.ValidString(v.message) {
		return HookConfigInvalid
	}
	b, err := codec.MarshalStable(v.wire())
	if err != nil || len(b) > MaxHookInputBytes {
		return HookInputLimit
	}
	return nil
}
func (v StopInput) Bytes() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(v.wire())
}
func (v StopInput) Sample() AssistantSampleReference { return v.sample }
func (v StopInput) Message() string                  { return v.message }
func (v StopInput) Active() bool                     { return v.active }
func DecodeStopInput(b []byte) (StopInput, error) {
	if len(b) > MaxHookInputBytes {
		return StopInput{}, HookInputLimit
	}
	var w stopInputWire
	if codec.DecodeObject(b, &w, "hook_event_name", "session_id", "thread_id", "turn_id", "sample_index", "cwd", "stop_hook_active", "last_assistant_message") != nil || w.Event != HookStop {
		return StopInput{}, HookConfigInvalid
	}
	return NewStopInput(w.Session, w.Thread, AssistantSampleReference{w.Turn, w.Index}, w.CWD, w.Active, w.Message)
}

// NewStopHookInput 把完成检查放入唯一当前运行输入，不制造工具字段。
func NewStopHookInput(input StopInput) (HookInput, error) {
	if err := input.Validate(); err != nil {
		return HookInput{}, err
	}
	v := HookInput{Event: HookStop, SessionID: input.session, ThreadID: input.thread, TurnID: input.sample.TurnID, CWD: input.cwd, stop: &input}
	return v, v.Validate()
}
func (v HookInput) StopInput() (StopInput, bool) {
	if v.Event != HookStop || v.stop == nil {
		return StopInput{}, false
	}
	return *v.stop, true
}

// MatchesTarget 校验本地运行输入与实际目标，不把 Provider call ID 当作 invocation。
func (v HookInput) MatchesTarget(target HookTarget) bool {
	if v.Validate() != nil || !target.ValidFor(v.Event) {
		return false
	}
	if v.Event == HookStop {
		sample, ok := target.AssistantSample()
		return ok && sample == v.stop.sample
	}
	_, cap, ok := target.ToolCall()
	return ok && cap == v.Input.Capability()
}

func (v HookInput) validateStop() error {
	if v.stop == nil || v.stop.Validate() != nil || v.SessionID != v.stop.session || v.ThreadID != v.stop.thread || v.TurnID != v.stop.sample.TurnID || v.CWD != v.stop.cwd || v.ToolUseID != "" || v.ToolName != "" || v.Input.capability != "" || v.Input.patch.FilePath != "" || len(v.Input.patch.Replacements) != 0 || v.Input.write != (hookWriteInput{}) || v.Input.exec != (hookExecInput{}) || v.Response != nil || v.Error != "" || v.IsInterrupt != nil {
		return HookConfigInvalid
	}
	return nil
}

// MarshalJSON 根据已校验事件选择封闭输入形状，Stop 不序列化任何工具字段。
func (v HookInput) MarshalJSON() ([]byte, error) {
	if v.Event == HookStop {
		if err := v.validateStop(); err != nil {
			return nil, err
		}
		return v.stop.Bytes()
	}
	if v.stop != nil {
		return nil, HookConfigInvalid
	}
	type toolInput HookInput
	return codec.MarshalStable(toolInput(v))
}

package domain

import (
	"encoding/json"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

// HookReplacement 保留当前 Patch 的规范语义，不传递 Provider wire。
type HookReplacement struct {
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll bool   `json:"replace_all"`
	LineBased  bool   `json:"line_based"`
}
type hookPatchInput struct {
	FilePath     string            `json:"file_path"`
	Replacements []HookReplacement `json:"replacements"`
}
type hookWriteInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}
type hookExecInput struct {
	Command       string `json:"command"`
	Workdir       string `json:"workdir"`
	TimeoutMillis int    `json:"timeout_ms"`
}

// HookToolInput 是只允许三种已实现能力的封闭输入。
type HookToolInput struct {
	capability HookCapability
	patch      hookPatchInput
	write      hookWriteInput
	exec       hookExecInput
}

func NewHookPatchInput(file string, replacements []HookReplacement) (HookToolInput, error) {
	v := HookToolInput{capability: HookPatch, patch: hookPatchInput{file, append([]HookReplacement(nil), replacements...)}}
	return v, v.Validate()
}
func NewHookWriteInput(file, content string) (HookToolInput, error) {
	v := HookToolInput{capability: HookWrite, write: hookWriteInput{file, content}}
	return v, v.Validate()
}
func NewHookExecInput(command, workdir string, timeout int) (HookToolInput, error) {
	v := HookToolInput{capability: HookExec, exec: hookExecInput{command, workdir, timeout}}
	return v, v.Validate()
}
func (v HookToolInput) Capability() HookCapability { return v.capability }
func (v HookToolInput) Clone() HookToolInput {
	v.patch.Replacements = append([]HookReplacement(nil), v.patch.Replacements...)
	return v
}
func hookRelativePath(s string, directory bool) bool {
	if !utf8.ValidString(s) || strings.ContainsAny(s, "\x00\\:") || len(s) > 4096 || path.IsAbs(s) {
		return false
	}
	if s == "" || s == "." {
		return directory
	}
	if path.Clean(s) != s {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if p == ".." {
			return false
		}
	}
	return true
}
func (v HookToolInput) Validate() error {
	switch v.capability {
	case HookPatch:
		if !hookRelativePath(v.patch.FilePath, false) || len(v.patch.Replacements) == 0 || len(v.patch.Replacements) > 1024 {
			return HookConfigInvalid
		}
		for _, r := range v.patch.Replacements {
			if !utf8.ValidString(r.OldText) || !utf8.ValidString(r.NewText) {
				return HookConfigInvalid
			}
		}
	case HookWrite:
		if !hookRelativePath(v.write.FilePath, false) || !utf8.ValidString(v.write.Content) {
			return HookConfigInvalid
		}
	case HookExec:
		if !utf8.ValidString(v.exec.Command) || strings.TrimSpace(v.exec.Command) == "" || strings.ContainsRune(v.exec.Command, 0) || !hookRelativePath(v.exec.Workdir, true) || v.exec.TimeoutMillis < 1 {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	b, err := v.MarshalJSON()
	if err != nil || len(b) > MaxHookInputBytes {
		return HookInputLimit
	}
	return nil
}
func (v HookToolInput) MarshalJSON() ([]byte, error) {
	switch v.capability {
	case HookPatch:
		return codec.MarshalStable(v.patch)
	case HookWrite:
		return codec.MarshalStable(v.write)
	case HookExec:
		return codec.MarshalStable(v.exec)
	default:
		return nil, HookConfigInvalid
	}
}
func DecodeHookToolInput(cap HookCapability, b []byte) (HookToolInput, error) {
	if len(b) > MaxHookInputBytes {
		return HookToolInput{}, HookInputLimit
	}
	switch cap {
	case HookPatch:
		var v hookPatchInput
		if codec.UnmarshalRecord(b, &v) != nil {
			return HookToolInput{}, HookConfigInvalid
		}
		return NewHookPatchInput(v.FilePath, v.Replacements)
	case HookWrite:
		var v hookWriteInput
		if codec.UnmarshalRecord(b, &v) != nil {
			return HookToolInput{}, HookConfigInvalid
		}
		return NewHookWriteInput(v.FilePath, v.Content)
	case HookExec:
		var v hookExecInput
		if codec.UnmarshalRecord(b, &v) != nil {
			return HookToolInput{}, HookConfigInvalid
		}
		return NewHookExecInput(v.Command, v.Workdir, v.TimeoutMillis)
	default:
		return HookToolInput{}, HookConfigInvalid
	}
}

// HookInput 是只发送给已信任本地检查的临时 stdin，不进入 Session。
type HookInput struct {
	stop        *StopInput
	Event       HookEvent         `json:"hook_event_name"`
	SessionID   SessionID         `json:"session_id"`
	ThreadID    ThreadID          `json:"thread_id"`
	TurnID      TurnID            `json:"turn_id"`
	ToolUseID   string            `json:"tool_use_id"`
	ToolName    string            `json:"tool_name"`
	CWD         string            `json:"cwd"`
	Input       HookToolInput     `json:"tool_input"`
	Response    *HookToolResponse `json:"tool_response,omitempty"`
	Error       string            `json:"error,omitempty"`
	IsInterrupt *bool             `json:"is_interrupt,omitempty"`
}

func NewHookInput(session SessionID, thread ThreadID, turn TurnID, call, name, cwd string, input HookToolInput) (HookInput, error) {
	v := HookInput{Event: HookPreToolUse, SessionID: session, ThreadID: thread, TurnID: turn, ToolUseID: call, ToolName: name, CWD: cwd, Input: input.Clone()}
	return v, v.Validate()
}
func (v HookInput) Validate() error {
	if v.Event == HookStop {
		return v.validateStop()
	}
	if v.stop != nil {
		return HookConfigInvalid
	}
	if !v.Event.Valid() || !v.SessionID.Valid() || !v.ThreadID.Valid() || !v.TurnID.Valid() || v.ToolUseID == "" || len(v.ToolUseID) > 256 || !utf8.ValidString(v.ToolUseID) || !filepath.IsAbs(v.CWD) || !utf8.ValidString(v.CWD) || strings.ContainsRune(v.CWD, 0) {
		return HookConfigInvalid
	}
	if v.Event == HookPreToolUse && v.Response != nil || v.Event == HookPostToolUse && (v.Response == nil || v.Response.Validate() != nil || v.Response.Capability() != v.Input.Capability()) {
		return HookConfigInvalid
	}
	if v.Event == HookPostToolUseFailure {
		if v.Response != nil || v.IsInterrupt == nil || *v.IsInterrupt || !strings.HasPrefix(v.Error, "Tool execution failed: ") || !safeHookFailureCode(strings.TrimPrefix(v.Error, "Tool execution failed: ")) {
			return HookConfigInvalid
		}
	} else if v.Error != "" || v.IsInterrupt != nil {
		return HookConfigInvalid
	}
	valid := v.Input.capability == HookPatch && (v.ToolName == "Edit" || v.ToolName == "apply_patch") || v.Input.capability == HookWrite && v.ToolName == "Write" || v.Input.capability == HookExec && (v.ToolName == "Bash" || v.ToolName == "exec_command")
	if !valid {
		return HookConfigInvalid
	}
	if err := v.Input.Validate(); err != nil {
		return err
	}
	b, err := codec.MarshalStable(v)
	if err != nil || len(b) > MaxHookInputBytes {
		return HookInputLimit
	}
	return nil
}
func (v HookInput) Bytes() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(v)
}

// NewPostHookInput 将真实成功结果投影与调用输入绑定，仅用于受信任的 stdin。
func NewPostHookInput(session SessionID, thread ThreadID, turn TurnID, call, name, cwd string, input HookToolInput, response HookToolResponse) (HookInput, error) {
	v := HookInput{Event: HookPostToolUse, SessionID: session, ThreadID: thread, TurnID: turn, ToolUseID: call, ToolName: name, CWD: cwd, Input: input.Clone(), Response: &response}
	return v, v.Validate()
}

func safeHookFailureCode(code string) bool {
	if len(code) == 0 || len(code) > 128 {
		return false
	}
	for _, c := range code {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// NewFailureHookInput 只从稳定错误分类生成本地摘要，不复制原错误或模型预览。
func NewFailureHookInput(session SessionID, thread ThreadID, turn TurnID, call, name, cwd string, input HookToolInput, code string) (HookInput, error) {
	if !safeHookFailureCode(code) {
		code = "tool_execution_failed"
	}
	interrupt := false
	v := HookInput{Event: HookPostToolUseFailure, SessionID: session, ThreadID: thread, TurnID: turn, ToolUseID: call, ToolName: name, CWD: cwd, Input: input.Clone(), Error: "Tool execution failed: " + code, IsInterrupt: &interrupt}
	return v, v.Validate()
}

// DecodeHookInput 严格解码各当前事件独有的输入形状，扩展字节仅用于封闭分支解码。
func DecodeHookInput(b []byte) (HookInput, error) {
	if len(b) > MaxHookInputBytes {
		return HookInput{}, HookInputLimit
	}
	members, err := codec.ObjectMembers(b)
	if err != nil {
		return HookInput{}, HookConfigInvalid
	}
	var event HookEvent
	if codec.UnmarshalRecord(members["hook_event_name"], &event) != nil {
		return HookInput{}, HookConfigInvalid
	}
	if event == HookStop {
		input, err := DecodeStopInput(b)
		if err != nil {
			return HookInput{}, err
		}
		return NewStopHookInput(input)
	}
	if len(b) > MaxHookInputBytes {
		return HookInput{}, HookInputLimit
	}
	var w struct {
		Event       HookEvent       `json:"hook_event_name"`
		SessionID   SessionID       `json:"session_id"`
		ThreadID    ThreadID        `json:"thread_id"`
		TurnID      TurnID          `json:"turn_id"`
		ToolUseID   string          `json:"tool_use_id"`
		ToolName    string          `json:"tool_name"`
		CWD         string          `json:"cwd"`
		Input       json.RawMessage `json:"tool_input"`
		Response    json.RawMessage `json:"tool_response,omitempty"`
		Error       string          `json:"error,omitempty"`
		IsInterrupt *bool           `json:"is_interrupt,omitempty"`
	}
	if codec.UnmarshalRecord(b, &w) != nil || !w.Event.Valid() {
		return HookInput{}, HookConfigInvalid
	}
	m, err := codec.ObjectMembers(b)
	if err != nil {
		return HookInput{}, HookConfigInvalid
	}
	_, response := m["tool_response"]
	_, failure := m["error"]
	_, interrupt := m["is_interrupt"]
	if response != (w.Event == HookPostToolUse) || failure != (w.Event == HookPostToolUseFailure) || interrupt != failure {
		return HookInput{}, HookConfigInvalid
	}
	capability := HookCapability("")
	switch w.ToolName {
	case "Edit", "apply_patch":
		capability = HookPatch
	case "Write":
		capability = HookWrite
	case "Bash", "exec_command":
		capability = HookExec
	}
	input, err := DecodeHookToolInput(capability, w.Input)
	if err != nil {
		return HookInput{}, err
	}
	v := HookInput{Event: w.Event, SessionID: w.SessionID, ThreadID: w.ThreadID, TurnID: w.TurnID, ToolUseID: w.ToolUseID, ToolName: w.ToolName, CWD: w.CWD, Input: input, Error: w.Error, IsInterrupt: w.IsInterrupt}
	if response {
		r, err := DecodeHookToolResponse(w.Response)
		if err != nil {
			return HookInput{}, err
		}
		v.Response = &r
	}
	return v, v.Validate()
}

package domain

import (
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
	Event     string        `json:"hook_event_name"`
	SessionID SessionID     `json:"session_id"`
	ThreadID  ThreadID      `json:"thread_id"`
	TurnID    TurnID        `json:"turn_id"`
	ToolUseID string        `json:"tool_use_id"`
	ToolName  string        `json:"tool_name"`
	CWD       string        `json:"cwd"`
	Input     HookToolInput `json:"tool_input"`
}

func NewHookInput(session SessionID, thread ThreadID, turn TurnID, call, name, cwd string, input HookToolInput) (HookInput, error) {
	v := HookInput{"PreToolUse", session, thread, turn, call, name, cwd, input.Clone()}
	return v, v.Validate()
}
func (v HookInput) Validate() error {
	if v.Event != "PreToolUse" || !v.SessionID.Valid() || !v.ThreadID.Valid() || !v.TurnID.Valid() || v.ToolUseID == "" || len(v.ToolUseID) > 256 || !utf8.ValidString(v.ToolUseID) || !filepath.IsAbs(v.CWD) || !utf8.ValidString(v.CWD) || strings.ContainsRune(v.CWD, 0) {
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

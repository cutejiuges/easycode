package domain

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

type compactionHookInput struct {
	id           CompactionID
	scope        CompactionScope
	instructions compactionHookInstructions
	summary      ContextSummary
}

type compactionHookInputWire struct {
	Event        HookEvent                   `json:"hook_event_name"`
	Session      SessionID                   `json:"session_id"`
	Thread       ThreadID                    `json:"thread_id"`
	ID           CompactionID                `json:"compaction_id"`
	CWD          string                      `json:"cwd"`
	Trigger      CompactionTrigger           `json:"trigger"`
	Instructions *compactionHookInstructions `json:"custom_instructions,omitempty"`
	Summary      string                      `json:"compact_summary,omitempty"`
	Turn         *TurnID                     `json:"turn_id,omitempty"`
	Index        *uint32                     `json:"sample_index,omitempty"`
}

// compactionHookInstructions 用 null 表达未提供原始用户重点。
type compactionHookInstructions string

func (v compactionHookInstructions) MarshalJSON() ([]byte, error) {
	if v == "" {
		return []byte("null"), nil
	}
	return codec.MarshalStable(string(v))
}

func (v *compactionHookInstructions) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*v = ""
		return nil
	}
	var text string
	if codec.UnmarshalRecord(b, &text) != nil || text == "" || strings.TrimSpace(text) != text {
		return HookConfigInvalid
	}
	*v = compactionHookInstructions(text)
	return nil
}

// NewPreCompactHookInput 冻结真实压缩身份和原始用户重点，不携带普通任务内容。
func NewPreCompactHookInput(session SessionID, thread ThreadID, id CompactionID, scope CompactionScope, cwd, originalFocus string) (HookInput, error) {
	v := HookInput{
		Event: HookPreCompact, SessionID: session, ThreadID: thread, TurnID: scope.TurnID, CWD: cwd,
		compaction: &compactionHookInput{id: id, scope: scope, instructions: compactionHookInstructions(strings.TrimSpace(originalFocus))},
	}
	return v, v.Validate()
}

// NewPostCompactHookInput 只向受信任脚本提供已安装摘要与实际压缩身份。
func NewPostCompactHookInput(session SessionID, thread ThreadID, id CompactionID, scope CompactionScope, cwd string, summary ContextSummary) (HookInput, error) {
	v := HookInput{
		Event: HookPostCompact, SessionID: session, ThreadID: thread, TurnID: scope.TurnID, CWD: cwd,
		compaction: &compactionHookInput{id: id, scope: scope, summary: summary},
	}
	return v, v.Validate()
}

func (v HookInput) compactionBytes() ([]byte, error) {
	input := v.compaction
	if input == nil || v.stop != nil || v.sessionStart != nil || v.promptSubmit != nil || v.hasToolFields() || !v.SessionID.Valid() || !v.ThreadID.Valid() ||
		!input.id.Valid() || input.scope.Validate() != nil || input.scope.TurnID != v.TurnID ||
		!filepath.IsAbs(v.CWD) || filepath.Clean(v.CWD) != v.CWD || !utf8.ValidString(v.CWD) || strings.ContainsRune(v.CWD, 0) {
		return nil, HookConfigInvalid
	}
	wire := compactionHookInputWire{
		Event: v.Event, Session: v.SessionID, Thread: v.ThreadID, ID: input.id, CWD: v.CWD,
		Trigger: input.scope.Trigger,
	}
	switch v.Event {
	case HookPreCompact:
		if input.summary.Text() != "" || !utf8.ValidString(string(input.instructions)) || input.scope.Trigger == CompactionAuto && input.instructions != "" {
			return nil, HookConfigInvalid
		}
		if len(input.instructions) > MaxCompactionInstructionsBytes {
			return nil, HookInputLimit
		}
		wire.Instructions = &input.instructions
	case HookPostCompact:
		if input.instructions != "" || input.summary.Text() == "" {
			return nil, HookConfigInvalid
		}
		wire.Summary = input.summary.Text()
	}
	if input.scope.Trigger == CompactionAuto {
		wire.Turn = &input.scope.TurnID
		wire.Index = &input.scope.SampleIndex
	}
	b, err := codec.MarshalStable(wire)
	if err != nil || len(b) > MaxHookInputBytes {
		return nil, HookInputLimit
	}
	return b, nil
}

func decodeCompactionHookInput(b []byte) (HookInput, error) {
	var wire compactionHookInputWire
	if codec.UnmarshalRecord(b, &wire) != nil {
		return HookInput{}, HookConfigInvalid
	}
	scope := ManualCompactionScope()
	switch wire.Trigger {
	case CompactionManual:
		if wire.Turn != nil || wire.Index != nil {
			return HookInput{}, HookConfigInvalid
		}
	case CompactionAuto:
		if wire.Turn == nil || wire.Index == nil {
			return HookInput{}, HookConfigInvalid
		}
		var err error
		scope, err = NewAutoCompactionScope(*wire.Turn, *wire.Index)
		if err != nil {
			return HookInput{}, HookConfigInvalid
		}
	default:
		return HookInput{}, HookConfigInvalid
	}
	if wire.Event == HookPostCompact {
		summary, err := NewContextSummary(wire.Summary)
		if err != nil {
			return HookInput{}, HookConfigInvalid
		}
		return NewPostCompactHookInput(wire.Session, wire.Thread, wire.ID, scope, wire.CWD, summary)
	}
	var instructions string
	if wire.Instructions != nil {
		instructions = string(*wire.Instructions)
	}
	return NewPreCompactHookInput(wire.Session, wire.Thread, wire.ID, scope, wire.CWD, instructions)
}

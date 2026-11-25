package domain

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

// PromptSubmitTarget 定位本次对话输入及预定的采样位置。
type PromptSubmitTarget struct {
	TurnID      TurnID  `json:"turn_id"`
	InputID     InputID `json:"input_id"`
	SampleIndex uint32  `json:"sample_index"`
}

func (v PromptSubmitTarget) Validate() error {
	if !v.TurnID.Valid() || !v.InputID.Valid() || v.SampleIndex >= 16 {
		return HookConfigInvalid
	}
	return nil
}

// ValidateUserPromptSubmitReferences 校验完整检查计划的有序声明引用。
func ValidateUserPromptSubmitReferences(refs []HookReference) error {
	if len(refs) == 0 || len(refs) > MaxHookMatches {
		return HookConfigInvalid
	}
	return validateHookReferences(HookUserPromptSubmit, refs)
}

// ValidUserPromptSubmitRejection 限定检查导致输入返还时可公开的原因。
func ValidUserPromptSubmitRejection(code HookCode) bool {
	switch code {
	case HookConfigInvalid, HookMatchLimit, HookTrustRequired, HookTrustDenied, HookDenied,
		HookInputLimit, HookOutputInvalid, HookOutputLimit, HookExitFailed, HookStartFailed,
		HookIOFailed, HookTimedOut, HookUnsupported, HookSandboxUnavailable, HookExecutionBudgetExhausted,
		HookOutcomeUncertain:
		return true
	default:
		return false
	}
}

type promptSubmitHookInput struct {
	id     InputID
	prompt string
}

type promptSubmitInputWire struct {
	Event   HookEvent `json:"hook_event_name"`
	Session SessionID `json:"session_id"`
	Thread  ThreadID  `json:"thread_id"`
	Turn    TurnID    `json:"turn_id"`
	Input   InputID   `json:"input_id"`
	CWD     string    `json:"cwd"`
	Prompt  string    `json:"prompt"`
}

// NewUserPromptSubmitHookInput 只把本次真实对话正文和运行定位交给已授权的脚本。
func NewUserPromptSubmitHookInput(session SessionID, thread ThreadID, target PromptSubmitTarget, cwd, prompt string) (HookInput, error) {
	if err := target.Validate(); err != nil {
		return HookInput{}, err
	}
	return makePromptSubmitHookInput(promptSubmitInputWire{HookUserPromptSubmit, session, thread, target.TurnID, target.InputID, cwd, prompt})
}

func makePromptSubmitHookInput(w promptSubmitInputWire) (HookInput, error) {
	v := HookInput{
		Event: w.Event, SessionID: w.Session, ThreadID: w.Thread, TurnID: w.Turn, CWD: w.CWD,
		promptSubmit: &promptSubmitHookInput{id: w.Input, prompt: w.Prompt},
	}
	if err := v.Validate(); err != nil {
		return HookInput{}, err
	}
	return v, nil
}

func (v HookInput) promptSubmitBytes() ([]byte, error) {
	input := v.promptSubmit
	if input == nil || v.stop != nil || v.compaction != nil || v.sessionStart != nil ||
		v.hasToolFields() || !v.SessionID.Valid() || !v.ThreadID.Valid() || !v.TurnID.Valid() || !input.id.Valid() ||
		!filepath.IsAbs(v.CWD) || filepath.Clean(v.CWD) != v.CWD || !utf8.ValidString(v.CWD) || strings.ContainsRune(v.CWD, 0) {
		return nil, HookConfigInvalid
	}
	if len(input.prompt) > MaxHookInputBytes {
		return nil, HookInputLimit
	}
	if ValidateUserText(input.prompt) != nil {
		return nil, HookConfigInvalid
	}
	wire := promptSubmitInputWire{v.Event, v.SessionID, v.ThreadID, v.TurnID, input.id, v.CWD, input.prompt}
	data, err := codec.MarshalStable(wire)
	if err != nil || len(data) > MaxHookInputBytes {
		return nil, HookInputLimit
	}
	return data, nil
}

func decodePromptSubmitHookInput(data []byte) (HookInput, error) {
	var wire promptSubmitInputWire
	if codec.UnmarshalRecord(data, &wire) != nil || wire.Event != HookUserPromptSubmit {
		return HookInput{}, HookConfigInvalid
	}
	return makePromptSubmitHookInput(wire)
}

func (r HookRunResult) validUserPromptSubmitResult() bool {
	const overflowBytes = MaxHookOutputBytes + 1
	if r.StdoutBytes > overflowBytes || r.StderrBytes > overflowBytes {
		return false
	}
	overflow := r.StdoutBytes == overflowBytes || r.StderrBytes == overflowBytes
	switch r.Code {
	case HookOutcomeUncertain:
		return !r.CleanupConfirmed
	case HookCancelled, HookTimedOut:
		return true
	case HookOutputLimit:
		return overflow
	case HookOK, HookDenied, HookExitFailed, HookStartFailed, HookOutputInvalid, HookIOFailed, HookUnsupported, HookSandboxUnavailable:
		return !overflow
	default:
		return false
	}
}

package protocol

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

// ToolActivityPayload 保存七工具活动的有界语义投影。
type ToolActivityPayload struct {
	Tool          ToolName `json:"tool"`
	Target        string   `json:"target"`
	InvocationID  string   `json:"invocation_id"`
	Summary       string   `json:"summary"`
	Workdir       string   `json:"workdir"`
	ElapsedMillis int64    `json:"elapsed_ms"`
	StdoutBytes   int64    `json:"stdout_bytes"`
	StderrBytes   int64    `json:"stderr_bytes"`
	StdoutLines   int64    `json:"stdout_lines"`
	StderrLines   int64    `json:"stderr_lines"`
	Recent        string   `json:"recent"`
	Truncated     bool     `json:"truncated"`
	Status        string   `json:"status"`
	Code          string   `json:"code"`
	ExitCode      int      `json:"exit_code"`
	ExitKnown     bool     `json:"exit_known"`
}

// ToolTerminalPayload 只在终态携带已持久化的有界结果，运行进度不包含该字段。
type ToolTerminalPayload struct {
	Stdout ToolOutputInfo `json:"stdout"`
	Stderr ToolOutputInfo `json:"stderr"`
	Search ToolOutputInfo `json:"search"`
	ToolActivityPayload
	ResultPreview string `json:"result_preview"`
}

func validateToolActivity(payload ToolActivityPayload, kind EventKind) error {
	if !payload.Tool.Valid() || len(payload.Target) > 4096 || !utf8.ValidString(payload.Target) || !validUUIDv7Text(payload.InvocationID) || payload.Summary == "" || utf8.RuneCountInString(payload.Summary) > 160 || !utf8.ValidString(payload.Summary) || strings.Count(payload.Summary, "\n") > 1 || (payload.Workdir != "" && !validApprovalPath(payload.Workdir)) || payload.ElapsedMillis < 0 || payload.StdoutBytes < 0 || payload.StderrBytes < 0 || payload.StdoutLines < 0 || payload.StderrLines < 0 || payload.StdoutLines > payload.StdoutBytes || payload.StderrLines > payload.StderrBytes || len(payload.Recent) > 4096 || !utf8.ValidString(payload.Recent) {
		return fmt.Errorf("invalid command activity")
	}
	if payload.Tool != ToolShell && (payload.Workdir != "" || payload.ElapsedMillis != 0 || payload.StdoutBytes != 0 || payload.StderrBytes != 0 || payload.StdoutLines != 0 || payload.StderrLines != 0 || payload.Recent != "" || payload.ExitKnown || payload.ExitCode != 0 || kind == EventToolProgress) {
		return fmt.Errorf("invalid non-command activity")
	}
	if kind == EventToolReady {
		if payload.Status != "waiting" || payload.Code != "" || payload.ExitKnown || payload.ExitCode != 0 {
			return fmt.Errorf("invalid ready tool activity")
		}
		return nil
	}
	if kind != EventToolTerminal {
		if payload.Status != "running" || payload.Code != "" || payload.ExitKnown || payload.ExitCode != 0 {
			return fmt.Errorf("invalid running command activity")
		}
		return nil
	}
	switch payload.Status {
	case "success", "denied", "conflict", "error", "cancelled", "outcome_uncertain":
	default:
		return fmt.Errorf("invalid command terminal status")
	}
	if !validCommandResultCode(payload.Code) || payload.ExitCode < -1 || payload.ExitCode > 255 || (!payload.ExitKnown && payload.ExitCode != 0) {
		return fmt.Errorf("invalid command terminal result")
	}
	if payload.Status == "success" && (payload.Code != "ok" || (payload.Tool == ToolShell && (!payload.ExitKnown || payload.ExitCode < 0))) {
		return fmt.Errorf("invalid successful command result")
	}
	if payload.Status == "denied" && (payload.ExitKnown || payload.ElapsedMillis != 0 || payload.StdoutBytes != 0 || payload.StderrBytes != 0 || payload.Recent != "") {
		return fmt.Errorf("denied command contains execution activity")
	}
	return nil
}

func validCommandResultCode(code string) bool {
	if code == "" || len(code) > 128 {
		return false
	}
	for _, character := range code {
		if character != '_' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// NewToolStarted 构造执行准入后的工具开始投影。
func NewToolStarted(payload ToolActivityPayload) (Event, error) {
	return newToolActivity(payload, EventToolStarted)
}

// NewToolProgress 构造非持久化命令进度投影。
func NewToolProgress(payload ToolActivityPayload) (Event, error) {
	return newToolActivity(payload, EventToolProgress)
}

// NewToolTerminal 构造资源清理且结果 durable 后的工具终态投影。
func NewToolTerminal(payload ToolTerminalPayload) (Event, error) {
	none := ToolOutputInfo{Summary: ToolOutputSummary{Status: "not_needed"}}
	if payload.Tool == ToolShell {
		if payload.Stdout == (ToolOutputInfo{}) {
			payload.Stdout = none
		}
		if payload.Stderr == (ToolOutputInfo{}) {
			payload.Stderr = none
		}
	}
	if payload.Tool == ToolGlob || payload.Tool == ToolGrep {
		if payload.Search == (ToolOutputInfo{}) {
			payload.Search = none
		}
	}
	if err := validateToolTerminal(payload); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventToolTerminal, payload)
}

func validateToolTerminal(payload ToolTerminalPayload) error {
	if err := validateTerminalSaving(payload); err != nil {
		return err
	}
	if (payload.Tool == ToolShell && len(payload.ResultPreview) == 0) || len(payload.ResultPreview) > 4096 || !utf8.ValidString(payload.ResultPreview) {
		return fmt.Errorf("invalid command result preview")
	}
	return validateToolActivity(payload.ToolActivityPayload, EventToolTerminal)
}

// DecodeToolTerminal 严格读取唯一当前终态契约，不接受缺失的冻结结果。
func DecodeToolTerminal(event Event) (ToolTerminalPayload, error) {
	var payload ToolTerminalPayload
	if event.Kind != EventToolTerminal || codec.UnmarshalRecord(event.Payload, &payload) != nil {
		return ToolTerminalPayload{}, fmt.Errorf("invalid command terminal payload")
	}
	if err := validateToolTerminal(payload); err != nil {
		return ToolTerminalPayload{}, err
	}
	for _, info := range []ToolOutputInfo{payload.Stdout, payload.Stderr, payload.Search} {
		if info.Reference != nil && event.ThreadID != "" && info.Reference.ThreadID != event.ThreadID {
			return ToolTerminalPayload{}, fmt.Errorf("terminal artifact thread mismatch")
		}
	}
	return payload, nil
}
func newToolActivity(payload ToolActivityPayload, kind EventKind) (Event, error) {
	if err := validateToolActivity(payload, kind); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(kind, payload)
}

// DecodeToolActivity 严格解析当前三个命令事件的共同投影。
func DecodeToolActivity(event Event) (ToolActivityPayload, error) {
	if event.Kind == EventToolTerminal {
		payload, err := DecodeToolTerminal(event)
		return payload.ToolActivityPayload, err
	}
	if event.Kind != EventToolReady && event.Kind != EventToolStarted && event.Kind != EventToolProgress && event.Kind != EventToolTerminal {
		return ToolActivityPayload{}, fmt.Errorf("not a command activity event")
	}
	var payload ToolActivityPayload
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return ToolActivityPayload{}, fmt.Errorf("invalid command activity payload")
	}
	if err := validateToolActivity(payload, event.Kind); err != nil {
		return ToolActivityPayload{}, err
	}
	return payload, nil
}

// NewToolReady 构造完整 ready 事实成功持久化后的等待投影。
func NewToolReady(payload ToolActivityPayload) (Event, error) {
	return newToolActivity(payload, EventToolReady)
}

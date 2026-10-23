package protocol

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

// CommandActivityPayload 保存命令时间线的有界语义投影。
type CommandActivityPayload struct {
	InvocationID  string `json:"invocation_id"`
	Command       string `json:"command"`
	Workdir       string `json:"workdir"`
	ElapsedMillis int64  `json:"elapsed_ms"`
	StdoutBytes   int64  `json:"stdout_bytes"`
	StderrBytes   int64  `json:"stderr_bytes"`
	StdoutLines   int64  `json:"stdout_lines"`
	StderrLines   int64  `json:"stderr_lines"`
	Recent        string `json:"recent"`
	Truncated     bool   `json:"truncated"`
	Status        string `json:"status"`
	Code          string `json:"code"`
	ExitCode      int    `json:"exit_code"`
	ExitKnown     bool   `json:"exit_known"`
}

// CommandTerminalPayload 只在终态携带已持久化的有界结果，运行进度不包含该字段。
type CommandTerminalPayload struct {
	CommandActivityPayload
	ResultPreview string `json:"result_preview"`
}

func validateCommandActivity(payload CommandActivityPayload, kind EventKind) error {
	if !validUUIDv7Text(payload.InvocationID) || payload.Command == "" || utf8.RuneCountInString(payload.Command) > 160 || !utf8.ValidString(payload.Command) || strings.Count(payload.Command, "\n") > 1 || (payload.Workdir != "" && !validApprovalPath(payload.Workdir)) || payload.ElapsedMillis < 0 || payload.StdoutBytes < 0 || payload.StderrBytes < 0 || payload.StdoutLines < 0 || payload.StderrLines < 0 || payload.StdoutLines > payload.StdoutBytes || payload.StderrLines > payload.StderrBytes || len(payload.Recent) > 4096 || !utf8.ValidString(payload.Recent) {
		return fmt.Errorf("invalid command activity")
	}
	if kind != EventCommandTerminal {
		if payload.Status != "running" || payload.Code != "" || payload.ExitKnown || payload.ExitCode != 0 {
			return fmt.Errorf("invalid running command activity")
		}
		return nil
	}
	switch payload.Status {
	case "success", "denied", "error", "cancelled", "outcome_uncertain":
	default:
		return fmt.Errorf("invalid command terminal status")
	}
	if !validCommandResultCode(payload.Code) || payload.ExitCode < -1 || payload.ExitCode > 255 || (!payload.ExitKnown && payload.ExitCode != 0) {
		return fmt.Errorf("invalid command terminal result")
	}
	if payload.Status == "success" && (payload.Code != "ok" || !payload.ExitKnown || payload.ExitCode < 0) {
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

// NewCommandStarted 构造执行准入后的命令开始投影。
func NewCommandStarted(payload CommandActivityPayload) (Event, error) {
	return newCommandActivity(payload, EventCommandStarted)
}

// NewCommandProgress 构造非持久化命令进度投影。
func NewCommandProgress(payload CommandActivityPayload) (Event, error) {
	return newCommandActivity(payload, EventCommandProgress)
}

// NewCommandTerminal 构造资源清理后的命令终态投影。
func NewCommandTerminal(payload CommandTerminalPayload) (Event, error) {
	if err := validateCommandTerminal(payload); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventCommandTerminal, payload)
}

func validateCommandTerminal(payload CommandTerminalPayload) error {
	if len(payload.ResultPreview) == 0 || len(payload.ResultPreview) > 256<<10 || !utf8.ValidString(payload.ResultPreview) {
		return fmt.Errorf("invalid command result preview")
	}
	return validateCommandActivity(payload.CommandActivityPayload, EventCommandTerminal)
}

// DecodeCommandTerminal 严格读取唯一当前终态契约，不接受缺失的冻结结果。
func DecodeCommandTerminal(event Event) (CommandTerminalPayload, error) {
	var payload CommandTerminalPayload
	if event.Kind != EventCommandTerminal || codec.UnmarshalStrict(event.Payload, &payload) != nil {
		return CommandTerminalPayload{}, fmt.Errorf("invalid command terminal payload")
	}
	if err := validateCommandTerminal(payload); err != nil {
		return CommandTerminalPayload{}, err
	}
	return payload, nil
}
func newCommandActivity(payload CommandActivityPayload, kind EventKind) (Event, error) {
	if err := validateCommandActivity(payload, kind); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(kind, payload)
}

// DecodeCommandActivity 严格解析当前三个命令事件的共同投影。
func DecodeCommandActivity(event Event) (CommandActivityPayload, error) {
	if event.Kind == EventCommandTerminal {
		payload, err := DecodeCommandTerminal(event)
		return payload.CommandActivityPayload, err
	}
	if event.Kind != EventCommandStarted && event.Kind != EventCommandProgress && event.Kind != EventCommandTerminal {
		return CommandActivityPayload{}, fmt.Errorf("not a command activity event")
	}
	var payload CommandActivityPayload
	if err := codec.UnmarshalStrict(event.Payload, &payload); err != nil {
		return CommandActivityPayload{}, fmt.Errorf("invalid command activity payload")
	}
	if err := validateCommandActivity(payload, event.Kind); err != nil {
		return CommandActivityPayload{}, err
	}
	return payload, nil
}

package protocol

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/domain"
)

const (
	// MaxInputBytes 是单条 Runtime 文本输入的 UTF-8 字节上限。
	MaxInputBytes = 4 << 20
	// MaxRequestIDBytes 是进程内关联标识的字节上限。
	MaxRequestIDBytes = 128
)

// CommandKind 描述宿主发送给 Runtime 的命令类型。
type CommandKind string

const (
	CommandSubmitInput         CommandKind = "submit_input"
	CommandInterrupt           CommandKind = "interrupt"
	CommandShutdown            CommandKind = "shutdown"
	CommandResolveToolApproval CommandKind = "resolve_tool_approval"
)

// RequestID 是调用方提供的进程内命令关联标识。
type RequestID string

// ParseRequestID 校验并返回不包含控制字符的 opaque request identity。
func ParseRequestID(value string) (RequestID, error) {
	if value == "" || len(value) > MaxRequestIDBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("request ID is invalid")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("request ID is invalid")
		}
	}
	return RequestID(value), nil
}

// Valid 判断 request identity 是否满足协议约束。
func (id RequestID) Valid() bool {
	_, err := ParseRequestID(string(id))
	return err == nil
}

// Command 是封闭的 Runtime 输入命令值对象。
type Command struct {
	kind              CommandKind
	requestID         RequestID
	text              string
	expectedTurnID    domain.TurnID
	approvalRequestID string
	invocationID      string
	fingerprint       string
	decision          ApprovalDecision
	feedback          string
}

// NewResolveToolApprovalCommand 创建与活动批准请求完整关联的响应命令。
func NewResolveToolApprovalCommand(requestID RequestID, expectedTurnID domain.TurnID, approvalRequestID, invocationID, fingerprint string, decision ApprovalDecision, feedback string) (Command, error) {
	command := Command{kind: CommandResolveToolApproval, requestID: requestID, expectedTurnID: expectedTurnID, approvalRequestID: approvalRequestID, invocationID: invocationID, fingerprint: fingerprint, decision: decision, feedback: strings.TrimSpace(feedback)}
	if err := command.Validate(); err != nil {
		return Command{}, err
	}
	return command, nil
}

// NewSubmitInputCommand 创建文本输入命令。
func NewSubmitInputCommand(requestID RequestID, text string) (Command, error) {
	command := Command{kind: CommandSubmitInput, requestID: requestID, text: text}
	if err := command.Validate(); err != nil {
		return Command{}, err
	}
	return command, nil
}

// NewInterruptCommand 创建只针对预期活动 turn 的中断命令。
func NewInterruptCommand(requestID RequestID, expectedTurnID domain.TurnID) (Command, error) {
	command := Command{kind: CommandInterrupt, requestID: requestID, expectedTurnID: expectedTurnID}
	if err := command.Validate(); err != nil {
		return Command{}, err
	}
	return command, nil
}

// NewShutdownCommand 创建显式关闭命令。
func NewShutdownCommand(requestID RequestID) (Command, error) {
	command := Command{kind: CommandShutdown, requestID: requestID}
	if err := command.Validate(); err != nil {
		return Command{}, err
	}
	return command, nil
}

// Kind 返回命令类型。
func (command Command) Kind() CommandKind { return command.kind }

// RequestID 返回命令关联标识。
func (command Command) RequestID() RequestID { return command.requestID }

// Text 返回 submit_input 文本；其他 kind 返回空字符串。
func (command Command) Text() string { return command.text }

// ExpectedTurnID 返回 interrupt 的目标；其他 kind 返回零值。
func (command Command) ExpectedTurnID() domain.TurnID      { return command.expectedTurnID }
func (command Command) ApprovalRequestID() string          { return command.approvalRequestID }
func (command Command) InvocationID() string               { return command.invocationID }
func (command Command) Fingerprint() string                { return command.fingerprint }
func (command Command) ApprovalDecision() ApprovalDecision { return command.decision }
func (command Command) Feedback() string                   { return command.feedback }

// Validate 验证命令 kind 和专属 payload。
func (command Command) Validate() error {
	if !command.requestID.Valid() {
		return fmt.Errorf("command request ID is invalid")
	}
	switch command.kind {
	case CommandSubmitInput:
		if command.expectedTurnID != "" || command.approvalRequestID != "" || command.invocationID != "" || command.fingerprint != "" || command.decision != "" || command.feedback != "" {
			return fmt.Errorf("submit command contains an interrupt target")
		}
		if err := validateInputText(command.text); err != nil {
			return err
		}
	case CommandInterrupt:
		if command.text != "" || !command.expectedTurnID.Valid() || command.approvalRequestID != "" || command.invocationID != "" || command.fingerprint != "" || command.decision != "" || command.feedback != "" {
			return fmt.Errorf("interrupt command payload is invalid")
		}
	case CommandShutdown:
		if command.text != "" || command.expectedTurnID != "" || command.approvalRequestID != "" || command.invocationID != "" || command.fingerprint != "" || command.decision != "" || command.feedback != "" {
			return fmt.Errorf("shutdown command contains a payload")
		}
	case CommandResolveToolApproval:
		if command.text != "" || !command.expectedTurnID.Valid() || !validUUIDv7Text(command.approvalRequestID) || !validUUIDv7Text(command.invocationID) || !validFingerprint(command.fingerprint) || !command.decision.Valid() || len(command.feedback) > 16<<10 || !utf8.ValidString(command.feedback) || (command.decision != ApprovalDeny && command.feedback != "") {
			return fmt.Errorf("approval command payload is invalid")
		}
	default:
		return fmt.Errorf("command kind is invalid")
	}
	return nil
}

func validateInputText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("command input is not valid UTF-8")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("command input is required")
	}
	if len(text) > MaxInputBytes {
		return fmt.Errorf("command input exceeds %d bytes", MaxInputBytes)
	}
	return nil
}

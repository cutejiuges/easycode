package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/codec"
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
	CommandSubmitInput CommandKind = "submit_input"
	CommandInterrupt   CommandKind = "interrupt"
	CommandShutdown    CommandKind = "shutdown"
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
	version        int
	kind           CommandKind
	requestID      RequestID
	text           string
	expectedTurnID domain.TurnID
}

// NewSubmitInputCommand 创建文本输入命令。
func NewSubmitInputCommand(requestID RequestID, text string) (Command, error) {
	command := Command{version: CurrentVersion, kind: CommandSubmitInput, requestID: requestID, text: text}
	if err := command.Validate(); err != nil {
		return Command{}, err
	}
	return command, nil
}

// NewInterruptCommand 创建只针对预期活动 turn 的中断命令。
func NewInterruptCommand(requestID RequestID, expectedTurnID domain.TurnID) (Command, error) {
	command := Command{version: CurrentVersion, kind: CommandInterrupt, requestID: requestID, expectedTurnID: expectedTurnID}
	if err := command.Validate(); err != nil {
		return Command{}, err
	}
	return command, nil
}

// NewShutdownCommand 创建显式关闭命令。
func NewShutdownCommand(requestID RequestID) (Command, error) {
	command := Command{version: CurrentVersion, kind: CommandShutdown, requestID: requestID}
	if err := command.Validate(); err != nil {
		return Command{}, err
	}
	return command, nil
}

// Version 返回命令协议版本。
func (command Command) Version() int { return command.version }

// Kind 返回命令类型。
func (command Command) Kind() CommandKind { return command.kind }

// RequestID 返回命令关联标识。
func (command Command) RequestID() RequestID { return command.requestID }

// Text 返回 submit_input 文本；其他 kind 返回空字符串。
func (command Command) Text() string { return command.text }

// ExpectedTurnID 返回 interrupt 的目标；其他 kind 返回零值。
func (command Command) ExpectedTurnID() domain.TurnID { return command.expectedTurnID }

// Validate 验证命令版本、kind 和专属 payload。
func (command Command) Validate() error {
	if command.version != CurrentVersion {
		return fmt.Errorf("command version is invalid")
	}
	if !command.requestID.Valid() {
		return fmt.Errorf("command request ID is invalid")
	}
	switch command.kind {
	case CommandSubmitInput:
		if command.expectedTurnID != "" {
			return fmt.Errorf("submit command contains an interrupt target")
		}
		if err := validateInputText(command.text); err != nil {
			return err
		}
	case CommandInterrupt:
		if command.text != "" || !command.expectedTurnID.Valid() {
			return fmt.Errorf("interrupt command payload is invalid")
		}
	case CommandShutdown:
		if command.text != "" || command.expectedTurnID != "" {
			return fmt.Errorf("shutdown command contains a payload")
		}
	default:
		return fmt.Errorf("command kind is invalid")
	}
	return nil
}

type commandHeader struct {
	Version   int         `json:"version"`
	Kind      CommandKind `json:"kind"`
	RequestID string      `json:"request_id"`
}

type submitInputCommandWire struct {
	Version   int         `json:"version"`
	Kind      CommandKind `json:"kind"`
	RequestID string      `json:"request_id"`
	Text      string      `json:"text"`
}

type interruptCommandWire struct {
	Version        int         `json:"version"`
	Kind           CommandKind `json:"kind"`
	RequestID      string      `json:"request_id"`
	ExpectedTurnID string      `json:"expected_turn_id"`
}

type shutdownCommandWire struct {
	Version   int         `json:"version"`
	Kind      CommandKind `json:"kind"`
	RequestID string      `json:"request_id"`
}

// DecodeCommand 严格解码一个且仅一个内部 Runtime 命令 JSON object。
func DecodeCommand(data []byte) (Command, error) {
	if !utf8.Valid(data) {
		return Command{}, fmt.Errorf("command JSON is not valid UTF-8")
	}
	if _, err := scanCommandMembers(data); err != nil {
		return Command{}, err
	}
	var header commandHeader
	if err := codec.UnmarshalStrict(data, &header); err != nil {
		// header 只用于分派，kind 专属字段在下一阶段解码。
		var permissive struct {
			Version   int         `json:"version"`
			Kind      CommandKind `json:"kind"`
			RequestID string      `json:"request_id"`
		}
		if decodeErr := json.Unmarshal(data, &permissive); decodeErr != nil {
			return Command{}, fmt.Errorf("decode command envelope: %w", decodeErr)
		}
		header = commandHeader(permissive)
	}
	if header.Version != CurrentVersion {
		return Command{}, fmt.Errorf("command version is unsupported")
	}
	requestID, err := ParseRequestID(header.RequestID)
	if err != nil {
		return Command{}, err
	}
	switch header.Kind {
	case CommandSubmitInput:
		var wire submitInputCommandWire
		if err := codec.UnmarshalStrict(data, &wire); err != nil {
			return Command{}, fmt.Errorf("submit command JSON is invalid")
		}
		return NewSubmitInputCommand(requestID, wire.Text)
	case CommandInterrupt:
		var wire interruptCommandWire
		if err := codec.UnmarshalStrict(data, &wire); err != nil {
			return Command{}, fmt.Errorf("interrupt command JSON is invalid")
		}
		turnID, err := domain.ParseTurnID(wire.ExpectedTurnID)
		if err != nil {
			return Command{}, fmt.Errorf("interrupt target is invalid")
		}
		return NewInterruptCommand(requestID, turnID)
	case CommandShutdown:
		var wire shutdownCommandWire
		if err := codec.UnmarshalStrict(data, &wire); err != nil {
			return Command{}, fmt.Errorf("shutdown command JSON is invalid")
		}
		return NewShutdownCommand(requestID)
	default:
		return Command{}, fmt.Errorf("command kind is unsupported")
	}
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

func scanCommandMembers(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode command object: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, fmt.Errorf("command must be a JSON object")
	}
	members := make(map[string]json.RawMessage, 5)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("decode command field: %w", err)
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("command field name is invalid")
		}
		if _, duplicate := members[name]; duplicate {
			return nil, fmt.Errorf("command contains a duplicate field")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("decode command field: %w", err)
		}
		members[name] = append(json.RawMessage(nil), raw...)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("close command object: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("command contains trailing JSON")
	}
	return members, nil
}

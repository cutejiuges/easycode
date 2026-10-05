package headless

import (
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

// StreamEvent 是长期 stream-json stdout v1 的封闭事件集合。
type StreamEvent interface {
	streamEvent()
}

// StreamThreadStartedEvent 表示流式会话已创建或恢复稳定 thread。
type StreamThreadStartedEvent struct {
	Version   int              `json:"version"`
	Type      string           `json:"type"`
	SessionID domain.SessionID `json:"session_id"`
	ThreadID  domain.ThreadID  `json:"thread_id"`
	Resumed   bool             `json:"resumed"`
}

// StreamControlError 是 command rejection 或 discard 的安全错误。
type StreamControlError struct {
	Code    protocol.ControlErrorCode `json:"code"`
	Message string                    `json:"message"`
}

// StreamControlResponseEvent 表示一条入站命令的 admission 结果。
type StreamControlResponseEvent struct {
	Version     int                         `json:"version"`
	Type        string                      `json:"type"`
	RequestID   protocol.RequestID          `json:"request_id"`
	CommandType string                      `json:"command_type"`
	Status      protocol.CommandStatus      `json:"status"`
	Disposition protocol.CommandDisposition `json:"disposition,omitempty"`
	Error       *StreamControlError         `json:"error,omitempty"`
}

// StreamTurnStartedEvent 表示某个 input 已 durable 开始独立 turn。
type StreamTurnStartedEvent struct {
	Version   int                `json:"version"`
	Type      string             `json:"type"`
	InputID   protocol.RequestID `json:"input_id"`
	SessionID domain.SessionID   `json:"session_id"`
	ThreadID  domain.ThreadID    `json:"thread_id"`
	TurnID    domain.TurnID      `json:"turn_id"`
}

// StreamAssistantTextDeltaEvent 表示某个 input 的文本增量。
type StreamAssistantTextDeltaEvent struct {
	Version   int                `json:"version"`
	Type      string             `json:"type"`
	InputID   protocol.RequestID `json:"input_id"`
	SessionID domain.SessionID   `json:"session_id"`
	ThreadID  domain.ThreadID    `json:"thread_id"`
	TurnID    domain.TurnID      `json:"turn_id"`
	Text      string             `json:"text"`
}

// StreamTurnCompletedEvent 表示某个 input 已 durable 成功。
type StreamTurnCompletedEvent struct {
	Version   int                `json:"version"`
	Type      string             `json:"type"`
	InputID   protocol.RequestID `json:"input_id"`
	SessionID domain.SessionID   `json:"session_id"`
	ThreadID  domain.ThreadID    `json:"thread_id"`
	TurnID    domain.TurnID      `json:"turn_id"`
	Usage     Usage              `json:"usage"`
}

// StreamTurnFailedEvent 表示某个 input 已 durable 失败。
type StreamTurnFailedEvent struct {
	Version   int                `json:"version"`
	Type      string             `json:"type"`
	InputID   protocol.RequestID `json:"input_id"`
	SessionID domain.SessionID   `json:"session_id"`
	ThreadID  domain.ThreadID    `json:"thread_id"`
	TurnID    domain.TurnID      `json:"turn_id"`
	Error     fault.Summary      `json:"error"`
}

// StreamInputDiscardedEvent 表示已接受但未启动的 input 被明确终止。
type StreamInputDiscardedEvent struct {
	Version int                `json:"version"`
	Type    string             `json:"type"`
	InputID protocol.RequestID `json:"input_id"`
	Error   StreamControlError `json:"error"`
}

// StreamErrorEvent 表示流式控制协议无法继续。
type StreamErrorEvent struct {
	Version int           `json:"version"`
	Type    string        `json:"type"`
	Error   fault.Summary `json:"error"`
}

func (StreamThreadStartedEvent) streamEvent()      {}
func (StreamControlResponseEvent) streamEvent()    {}
func (StreamTurnStartedEvent) streamEvent()        {}
func (StreamAssistantTextDeltaEvent) streamEvent() {}
func (StreamTurnCompletedEvent) streamEvent()      {}
func (StreamTurnFailedEvent) streamEvent()         {}
func (StreamInputDiscardedEvent) streamEvent()     {}
func (StreamErrorEvent) streamEvent()              {}

// NewStreamThreadStartedEvent 创建流式 thread.started。
func NewStreamThreadStartedEvent(sessionID domain.SessionID, threadID domain.ThreadID, resumed bool) (StreamThreadStartedEvent, error) {
	if err := validateThreadIdentity(sessionID, threadID); err != nil {
		return StreamThreadStartedEvent{}, err
	}
	return StreamThreadStartedEvent{
		Version: eventVersion, Type: "thread.started", SessionID: sessionID, ThreadID: threadID, Resumed: resumed,
	}, nil
}

// NewStreamErrorEvent 创建不包含内部 cause 的终止性 error。
func NewStreamErrorEvent(failure fault.Summary) (StreamErrorEvent, error) {
	if err := validateFailure(failure); err != nil {
		return StreamErrorEvent{}, err
	}
	return StreamErrorEvent{Version: eventVersion, Type: "error", Error: failure}, nil
}

// StreamProjector 将内部 control item 投影为独立 wire DTO。
type StreamProjector struct {
	sessionID  domain.SessionID
	threadID   domain.ThreadID
	projectors map[protocol.RequestID]*projector
}

// NewStreamProjector 创建不执行 I/O 的有状态流式 projector。
func NewStreamProjector(sessionID domain.SessionID, threadID domain.ThreadID) (*StreamProjector, error) {
	if err := validateThreadIdentity(sessionID, threadID); err != nil {
		return nil, err
	}
	return &StreamProjector{
		sessionID: sessionID, threadID: threadID,
		projectors: make(map[protocol.RequestID]*projector),
	}, nil
}

// Project 验证并投影一个 owner 已排序的 control item。
func (projectorValue *StreamProjector) Project(item protocol.ControlItem) (StreamEvent, error) {
	if projectorValue == nil || projectorValue.projectors == nil {
		return nil, fmt.Errorf("stream projector is unavailable")
	}
	if err := item.Validate(); err != nil {
		return nil, fmt.Errorf("control item is invalid")
	}
	switch item.Kind() {
	case protocol.ControlItemCommandResult:
		result, _ := item.CommandResult()
		return projectCommandResult(result)
	case protocol.ControlItemTurnEvent:
		runtimeEvent, _ := item.TurnEvent()
		if runtimeEvent.SessionID != projectorValue.sessionID || runtimeEvent.ThreadID != projectorValue.threadID {
			return nil, fmt.Errorf("stream event identity is invalid")
		}
		current := projectorValue.projectors[item.RequestID()]
		if current == nil {
			current = newProjector(projectorValue.sessionID, projectorValue.threadID)
			projectorValue.projectors[item.RequestID()] = current
		}
		projected, err := current.project(runtimeEvent)
		if err != nil {
			return nil, err
		}
		wire, err := correlateProjectedEvent(item.RequestID(), projected.event)
		if projected.terminal {
			delete(projectorValue.projectors, item.RequestID())
		}
		return wire, err
	case protocol.ControlItemInputDiscard:
		discard, _ := item.DiscardError()
		return StreamInputDiscardedEvent{
			Version: eventVersion, Type: "input.discarded", InputID: item.RequestID(),
			Error: StreamControlError{Code: discard.Code(), Message: discard.Message()},
		}, nil
	default:
		return nil, fmt.Errorf("control item kind is unsupported")
	}
}

func projectCommandResult(result protocol.CommandResult) (StreamEvent, error) {
	commandType, ok := streamCommandType(result.CommandKind())
	if !ok {
		return nil, fmt.Errorf("command result kind is invalid")
	}
	event := StreamControlResponseEvent{
		Version: eventVersion, Type: "control.response", RequestID: result.RequestID(),
		CommandType: commandType, Status: result.Status(), Disposition: result.Disposition(),
	}
	if summary, rejected := result.Error(); rejected {
		event.Error = &StreamControlError{Code: summary.Code(), Message: summary.Message()}
	}
	return event, validateStreamEvent(event)
}

func correlateProjectedEvent(inputID protocol.RequestID, event Event) (StreamEvent, error) {
	if !inputID.Valid() {
		return nil, fmt.Errorf("stream input ID is invalid")
	}
	switch value := event.(type) {
	case TurnStartedEvent:
		return StreamTurnStartedEvent{
			Version: value.Version, Type: value.Type, InputID: inputID,
			SessionID: value.SessionID, ThreadID: value.ThreadID, TurnID: value.TurnID,
		}, nil
	case AssistantTextDeltaEvent:
		return StreamAssistantTextDeltaEvent{
			Version: value.Version, Type: value.Type, InputID: inputID,
			SessionID: value.SessionID, ThreadID: value.ThreadID, TurnID: value.TurnID, Text: value.Text,
		}, nil
	case TurnCompletedEvent:
		return StreamTurnCompletedEvent{
			Version: value.Version, Type: value.Type, InputID: inputID,
			SessionID: value.SessionID, ThreadID: value.ThreadID, TurnID: value.TurnID, Usage: value.Usage,
		}, nil
	case TurnFailedEvent:
		return StreamTurnFailedEvent{
			Version: value.Version, Type: value.Type, InputID: inputID,
			SessionID: value.SessionID, ThreadID: value.ThreadID, TurnID: value.TurnID, Error: value.Error,
		}, nil
	default:
		return nil, fmt.Errorf("projected stream event is unsupported")
	}
}

func streamCommandType(kind protocol.CommandKind) (string, bool) {
	switch kind {
	case protocol.CommandSubmitInput:
		return "input.submit", true
	case protocol.CommandInterrupt:
		return "turn.interrupt", true
	case protocol.CommandShutdown:
		return "session.shutdown", true
	default:
		return "", false
	}
}

func validateStreamEvent(event StreamEvent) error {
	switch value := event.(type) {
	case StreamThreadStartedEvent:
		if value.Version != eventVersion || value.Type != "thread.started" {
			return fmt.Errorf("stream thread.started envelope is invalid")
		}
		return validateThreadIdentity(value.SessionID, value.ThreadID)
	case StreamControlResponseEvent:
		if value.Version != eventVersion || value.Type != "control.response" || !value.RequestID.Valid() {
			return fmt.Errorf("stream control.response envelope is invalid")
		}
		if value.CommandType != "input.submit" && value.CommandType != "turn.interrupt" && value.CommandType != "session.shutdown" {
			return fmt.Errorf("stream control.response command type is invalid")
		}
		if value.Status == protocol.CommandStatusAccepted {
			if value.Error != nil || value.Disposition == "" {
				return fmt.Errorf("stream accepted response is invalid")
			}
			return nil
		}
		if value.Status != protocol.CommandStatusRejected || value.Disposition != "" || value.Error == nil || value.Error.Code == "" || value.Error.Message == "" {
			return fmt.Errorf("stream rejected response is invalid")
		}
		return nil
	case StreamTurnStartedEvent:
		if value.Version != eventVersion || value.Type != "turn.started" || !value.InputID.Valid() {
			return fmt.Errorf("stream turn.started envelope is invalid")
		}
		return validateTurnIdentity(value.SessionID, value.ThreadID, value.TurnID)
	case StreamAssistantTextDeltaEvent:
		if value.Version != eventVersion || value.Type != "assistant.text.delta" || !value.InputID.Valid() || value.Text == "" {
			return fmt.Errorf("stream assistant.text.delta envelope is invalid")
		}
		return validateTurnIdentity(value.SessionID, value.ThreadID, value.TurnID)
	case StreamTurnCompletedEvent:
		if value.Version != eventVersion || value.Type != "turn.completed" || !value.InputID.Valid() {
			return fmt.Errorf("stream turn.completed envelope is invalid")
		}
		if err := validateTurnIdentity(value.SessionID, value.ThreadID, value.TurnID); err != nil {
			return err
		}
		_, err := value.Usage.Domain()
		return err
	case StreamTurnFailedEvent:
		if value.Version != eventVersion || value.Type != "turn.failed" || !value.InputID.Valid() {
			return fmt.Errorf("stream turn.failed envelope is invalid")
		}
		if err := validateTurnIdentity(value.SessionID, value.ThreadID, value.TurnID); err != nil {
			return err
		}
		return validateFailure(value.Error)
	case StreamInputDiscardedEvent:
		if value.Version != eventVersion || value.Type != "input.discarded" || !value.InputID.Valid() || value.Error.Code == "" || value.Error.Message == "" {
			return fmt.Errorf("stream input.discarded envelope is invalid")
		}
		return nil
	case StreamErrorEvent:
		if value.Version != eventVersion || value.Type != "error" {
			return fmt.Errorf("stream error envelope is invalid")
		}
		return validateFailure(value.Error)
	default:
		return fmt.Errorf("stream event type is unsupported")
	}
}

// StreamEncoder 是长期控制 stdout 的唯一 JSONL writer。
type StreamEncoder struct {
	writer io.Writer
	failed bool
}

// NewStreamEncoder 创建不执行 I/O 的流式 encoder。
func NewStreamEncoder(writer io.Writer) *StreamEncoder {
	return &StreamEncoder{writer: writer}
}

// Write 写入一个完整 canonical JSON object 和换行；首次失败后永久拒绝写入。
func (encoder *StreamEncoder) Write(event StreamEvent) error {
	if encoder == nil || encoder.writer == nil || encoder.failed {
		return fmt.Errorf("stream output is unavailable")
	}
	if err := validateStreamEvent(event); err != nil {
		return err
	}
	encoded, err := codec.MarshalStable(event)
	if err != nil || !utf8.Valid(encoded) || bytes.ContainsAny(encoded, "\r\n") {
		encoder.failed = true
		return fmt.Errorf("encode stream event failed")
	}
	frame := append(append(make([]byte, 0, len(encoded)+1), encoded...), '\n')
	if err := writeFull(encoder.writer, frame); err != nil {
		encoder.failed = true
		return fmt.Errorf("write stream event failed")
	}
	return nil
}

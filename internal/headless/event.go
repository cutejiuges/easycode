package headless

import (
	"fmt"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/fault"
)

const eventVersion = 1

// Event 是 JSONL v1 的封闭事件集合。
type Event interface {
	headlessEvent()
}

// ThreadStartedEvent 表示 headless 已创建或恢复稳定 thread。
type ThreadStartedEvent struct {
	Version   int              `json:"version"`
	Type      string           `json:"type"`
	SessionID domain.SessionID `json:"session_id"`
	ThreadID  domain.ThreadID  `json:"thread_id"`
	Resumed   bool             `json:"resumed"`
}

// TurnStartedEvent 表示本次新 turn 已 durable 开始。
type TurnStartedEvent struct {
	Version   int              `json:"version"`
	Type      string           `json:"type"`
	SessionID domain.SessionID `json:"session_id"`
	ThreadID  domain.ThreadID  `json:"thread_id"`
	TurnID    domain.TurnID    `json:"turn_id"`
}

// AssistantTextDeltaEvent 表示本次 turn 的文本增量。
type AssistantTextDeltaEvent struct {
	Version   int              `json:"version"`
	Type      string           `json:"type"`
	SessionID domain.SessionID `json:"session_id"`
	ThreadID  domain.ThreadID  `json:"thread_id"`
	TurnID    domain.TurnID    `json:"turn_id"`
	Text      string           `json:"text"`
}

// TurnCompletedEvent 表示本次 turn 已 durable 成功。
type TurnCompletedEvent struct {
	Version   int              `json:"version"`
	Type      string           `json:"type"`
	SessionID domain.SessionID `json:"session_id"`
	ThreadID  domain.ThreadID  `json:"thread_id"`
	TurnID    domain.TurnID    `json:"turn_id"`
	Usage     Usage            `json:"usage"`
}

// TurnFailedEvent 表示本次 turn 已 durable 失败。
type TurnFailedEvent struct {
	Version   int              `json:"version"`
	Type      string           `json:"type"`
	SessionID domain.SessionID `json:"session_id"`
	ThreadID  domain.ThreadID  `json:"thread_id"`
	TurnID    domain.TurnID    `json:"turn_id"`
	Error     fault.Summary    `json:"error"`
}

// ErrorEvent 表示无法继续建立或解释事件流的错误。
type ErrorEvent struct {
	Version int           `json:"version"`
	Type    string        `json:"type"`
	Error   fault.Summary `json:"error"`
}

func (ThreadStartedEvent) headlessEvent()      {}
func (TurnStartedEvent) headlessEvent()        {}
func (AssistantTextDeltaEvent) headlessEvent() {}
func (TurnCompletedEvent) headlessEvent()      {}
func (TurnFailedEvent) headlessEvent()         {}
func (ErrorEvent) headlessEvent()              {}

// NewThreadStartedEvent 创建 thread.started 事件。
func NewThreadStartedEvent(sessionID domain.SessionID, threadID domain.ThreadID, resumed bool) (ThreadStartedEvent, error) {
	if err := validateThreadIdentity(sessionID, threadID); err != nil {
		return ThreadStartedEvent{}, err
	}
	return ThreadStartedEvent{
		Version: eventVersion, Type: "thread.started",
		SessionID: sessionID, ThreadID: threadID, Resumed: resumed,
	}, nil
}

// NewTurnStartedEvent 创建 turn.started 事件。
func NewTurnStartedEvent(sessionID domain.SessionID, threadID domain.ThreadID, turnID domain.TurnID) (TurnStartedEvent, error) {
	if err := validateTurnIdentity(sessionID, threadID, turnID); err != nil {
		return TurnStartedEvent{}, err
	}
	return TurnStartedEvent{
		Version: eventVersion, Type: "turn.started",
		SessionID: sessionID, ThreadID: threadID, TurnID: turnID,
	}, nil
}

// NewAssistantTextDeltaEvent 创建 assistant.text.delta 事件。
func NewAssistantTextDeltaEvent(
	sessionID domain.SessionID,
	threadID domain.ThreadID,
	turnID domain.TurnID,
	text string,
) (AssistantTextDeltaEvent, error) {
	if err := validateTurnIdentity(sessionID, threadID, turnID); err != nil {
		return AssistantTextDeltaEvent{}, err
	}
	if text == "" {
		return AssistantTextDeltaEvent{}, fmt.Errorf("assistant text delta is required")
	}
	return AssistantTextDeltaEvent{
		Version: eventVersion, Type: "assistant.text.delta",
		SessionID: sessionID, ThreadID: threadID, TurnID: turnID, Text: text,
	}, nil
}

// NewTurnCompletedEvent 创建 turn.completed 事件。
func NewTurnCompletedEvent(
	sessionID domain.SessionID,
	threadID domain.ThreadID,
	turnID domain.TurnID,
	turnUsage domain.SampleUsage,
) (TurnCompletedEvent, error) {
	if err := validateTurnIdentity(sessionID, threadID, turnID); err != nil {
		return TurnCompletedEvent{}, err
	}
	usage, err := newUsage(turnUsage)
	if err != nil {
		return TurnCompletedEvent{}, err
	}
	return TurnCompletedEvent{
		Version: eventVersion, Type: "turn.completed",
		SessionID: sessionID, ThreadID: threadID, TurnID: turnID, Usage: usage,
	}, nil
}

// NewTurnFailedEvent 创建 turn.failed 事件。
func NewTurnFailedEvent(
	sessionID domain.SessionID,
	threadID domain.ThreadID,
	turnID domain.TurnID,
	failure fault.Summary,
) (TurnFailedEvent, error) {
	if err := validateTurnIdentity(sessionID, threadID, turnID); err != nil {
		return TurnFailedEvent{}, err
	}
	if err := validateFailure(failure); err != nil {
		return TurnFailedEvent{}, err
	}
	return TurnFailedEvent{
		Version: eventVersion, Type: "turn.failed",
		SessionID: sessionID, ThreadID: threadID, TurnID: turnID, Error: failure,
	}, nil
}

// NewErrorEvent 创建 stream-level error 事件。
func NewErrorEvent(failure fault.Summary) (ErrorEvent, error) {
	if err := validateFailure(failure); err != nil {
		return ErrorEvent{}, err
	}
	return ErrorEvent{Version: eventVersion, Type: "error", Error: failure}, nil
}

func validateThreadIdentity(sessionID domain.SessionID, threadID domain.ThreadID) error {
	if !sessionID.Valid() || !threadID.Valid() {
		return fmt.Errorf("headless thread identity is invalid")
	}
	return nil
}

func validateTurnIdentity(sessionID domain.SessionID, threadID domain.ThreadID, turnID domain.TurnID) error {
	if err := validateThreadIdentity(sessionID, threadID); err != nil {
		return err
	}
	if !turnID.Valid() {
		return fmt.Errorf("headless turn identity is invalid")
	}
	return nil
}

func validateFailure(failure fault.Summary) error {
	if strings.TrimSpace(string(failure.Code)) == "" || strings.TrimSpace(failure.Message) == "" {
		return fmt.Errorf("headless failure is incomplete")
	}
	return nil
}

func validateEvent(event Event) error {
	switch value := event.(type) {
	case ThreadStartedEvent:
		if value.Version != eventVersion || value.Type != "thread.started" {
			return fmt.Errorf("thread.started envelope is invalid")
		}
		return validateThreadIdentity(value.SessionID, value.ThreadID)
	case TurnStartedEvent:
		if value.Version != eventVersion || value.Type != "turn.started" {
			return fmt.Errorf("turn.started envelope is invalid")
		}
		return validateTurnIdentity(value.SessionID, value.ThreadID, value.TurnID)
	case AssistantTextDeltaEvent:
		if value.Version != eventVersion || value.Type != "assistant.text.delta" || value.Text == "" {
			return fmt.Errorf("assistant.text.delta envelope is invalid")
		}
		return validateTurnIdentity(value.SessionID, value.ThreadID, value.TurnID)
	case TurnCompletedEvent:
		if value.Version != eventVersion || value.Type != "turn.completed" {
			return fmt.Errorf("turn.completed envelope is invalid")
		}
		if err := validateTurnIdentity(value.SessionID, value.ThreadID, value.TurnID); err != nil {
			return err
		}
		_, err := value.Usage.Domain()
		return err
	case TurnFailedEvent:
		if value.Version != eventVersion || value.Type != "turn.failed" {
			return fmt.Errorf("turn.failed envelope is invalid")
		}
		if err := validateTurnIdentity(value.SessionID, value.ThreadID, value.TurnID); err != nil {
			return err
		}
		return validateFailure(value.Error)
	case ErrorEvent:
		if value.Version != eventVersion || value.Type != "error" {
			return fmt.Errorf("error envelope is invalid")
		}
		return validateFailure(value.Error)
	default:
		return fmt.Errorf("headless event type is unsupported")
	}
}

package protocol

import (
	"fmt"
	"strings"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// AssistantTextDeltaPayload 保存一次 assistant 文本增量。
type AssistantTextDeltaPayload struct {
	Text string `json:"text"`
}

// TurnFailedPayload 保存可供宿主安全展示的 turn 失败摘要。
type TurnFailedPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// TurnCompletedPayload 保存成功 turn 的归一化 usage。
type TurnCompletedPayload struct {
	Usage UsagePayload `json:"usage"`
}

func newEventWithPayload(kind EventKind, payload any) (Event, error) {
	encoded, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal runtime event payload: %w", err)
	}
	event := newEvent(kind)
	event.Payload = encoded
	return event, nil
}

// NewTurnStarted 创建 turn 开始事件。
func NewTurnStarted() Event {
	return newEvent(EventTurnStarted)
}

// ValidateTurnStarted 严格校验 turn 开始事件。
func ValidateTurnStarted(event Event) error {
	return validatePayloadlessEvent(event, EventTurnStarted)
}

// NewAssistantTextDelta 创建 assistant 文本增量事件。
func NewAssistantTextDelta(text string) (Event, error) {
	if text == "" {
		return Event{}, fmt.Errorf("assistant text delta is required")
	}
	return newEventWithPayload(EventAssistantTextDelta, AssistantTextDeltaPayload{Text: text})
}

// DecodeAssistantTextDelta 解码并校验 assistant 文本增量。
func DecodeAssistantTextDelta(event Event) (AssistantTextDeltaPayload, error) {
	if event.Kind != EventAssistantTextDelta {
		return AssistantTextDeltaPayload{}, fmt.Errorf("event kind is not assistant_text_delta")
	}
	var payload AssistantTextDeltaPayload
	if err := codec.UnmarshalStrict(event.Payload, &payload); err != nil {
		return AssistantTextDeltaPayload{}, fmt.Errorf("decode assistant text delta: %w", err)
	}
	if payload.Text == "" {
		return AssistantTextDeltaPayload{}, fmt.Errorf("assistant text delta is required")
	}
	return payload, nil
}

// NewTurnFailed 创建安全的 turn 失败事件。
func NewTurnFailed(code string, message string, cancelled bool) (Event, error) {
	if strings.TrimSpace(code) == "" {
		return Event{}, fmt.Errorf("turn failure code is required")
	}
	if strings.TrimSpace(message) == "" {
		return Event{}, fmt.Errorf("turn failure message is required")
	}
	return newEventWithPayload(EventTurnFailed, TurnFailedPayload{
		Code:      code,
		Message:   message,
		Cancelled: cancelled,
	})
}

// DecodeTurnFailed 解码并校验 turn 失败事件。
func DecodeTurnFailed(event Event) (TurnFailedPayload, error) {
	if event.Kind != EventTurnFailed {
		return TurnFailedPayload{}, fmt.Errorf("event kind is not turn_failed")
	}
	var payload TurnFailedPayload
	if err := codec.UnmarshalStrict(event.Payload, &payload); err != nil {
		return TurnFailedPayload{}, fmt.Errorf("decode turn failure: %w", err)
	}
	if strings.TrimSpace(payload.Code) == "" || strings.TrimSpace(payload.Message) == "" {
		return TurnFailedPayload{}, fmt.Errorf("turn failure payload is incomplete")
	}
	return payload, nil
}

// NewTurnCompleted 创建携带 turn usage 的成功完成事件。
func NewTurnCompleted(usage domain.SampleUsage) (Event, error) {
	payload, err := newUsagePayload(usage)
	if err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventTurnCompleted, TurnCompletedPayload{Usage: payload})
}

// DecodeTurnCompleted 解码并校验 turn 成功完成事件。
func DecodeTurnCompleted(event Event) (TurnCompletedPayload, error) {
	if event.Kind != EventTurnCompleted {
		return TurnCompletedPayload{}, fmt.Errorf("event kind is not turn_completed")
	}
	var payload TurnCompletedPayload
	if err := codec.UnmarshalStrict(event.Payload, &payload); err != nil {
		return TurnCompletedPayload{}, fmt.Errorf("decode turn completion: %w", err)
	}
	if _, err := payload.Usage.Domain(); err != nil {
		return TurnCompletedPayload{}, fmt.Errorf("turn completion usage is invalid: %w", err)
	}
	return payload, nil
}

func validatePayloadlessEvent(event Event, kind EventKind) error {
	if event.Kind != kind {
		return fmt.Errorf("event kind is not %s", kind)
	}
	if len(event.Payload) != 0 {
		return fmt.Errorf("%s payload must be empty", kind)
	}
	return nil
}

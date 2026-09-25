package protocol

import (
	"fmt"

	"easycode/internal/codec"
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

// NewEventWithPayload 创建携带强类型 payload 的版本化事件。
func NewEventWithPayload(kind EventKind, payload any) (Event, error) {
	encoded, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, fmt.Errorf("marshal runtime event payload: %w", err)
	}
	event := NewEvent(kind)
	event.Payload = encoded
	return event, nil
}

// NewAssistantTextDelta 创建 assistant 文本增量事件。
func NewAssistantTextDelta(text string) (Event, error) {
	if text == "" {
		return Event{}, fmt.Errorf("assistant text delta is required")
	}
	return NewEventWithPayload(EventAssistantTextDelta, AssistantTextDeltaPayload{Text: text})
}

// DecodeAssistantTextDelta 解码并校验 assistant 文本增量。
func DecodeAssistantTextDelta(event Event) (AssistantTextDeltaPayload, error) {
	if event.Kind != EventAssistantTextDelta {
		return AssistantTextDeltaPayload{}, fmt.Errorf("event kind is not assistant_text_delta")
	}
	var payload AssistantTextDeltaPayload
	if err := codec.Unmarshal(event.Payload, &payload); err != nil {
		return AssistantTextDeltaPayload{}, fmt.Errorf("decode assistant text delta: %w", err)
	}
	if payload.Text == "" {
		return AssistantTextDeltaPayload{}, fmt.Errorf("assistant text delta is required")
	}
	return payload, nil
}

// NewTurnFailed 创建安全的 turn 失败事件。
func NewTurnFailed(code string, message string, cancelled bool) (Event, error) {
	if code == "" {
		return Event{}, fmt.Errorf("turn failure code is required")
	}
	if message == "" {
		return Event{}, fmt.Errorf("turn failure message is required")
	}
	return NewEventWithPayload(EventTurnFailed, TurnFailedPayload{
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
	if err := codec.Unmarshal(event.Payload, &payload); err != nil {
		return TurnFailedPayload{}, fmt.Errorf("decode turn failure: %w", err)
	}
	if payload.Code == "" || payload.Message == "" {
		return TurnFailedPayload{}, fmt.Errorf("turn failure payload is incomplete")
	}
	return payload, nil
}

// Package protocol 定义 Runtime 与宿主之间可版本化的 command/event 协议。
package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"easycode/internal/domain"
)

const CurrentVersion = 1

// EventKind 描述共享语义事件类型，不包含 provider wire 细节。
type EventKind string

const (
	EventTurnStarted        EventKind = "turn_started"
	EventAssistantTextDelta EventKind = "assistant_text_delta"
	EventTurnCompleted      EventKind = "turn_completed"
	EventTurnFailed         EventKind = "turn_failed"
)

// Event 是 TUI、headless 和其他宿主共同消费的进程内语义事件信封。
type Event struct {
	Version   int              `json:"version"`
	Kind      EventKind        `json:"kind"`
	Timestamp time.Time        `json:"timestamp"`
	SessionID domain.SessionID `json:"session_id,omitempty"`
	ThreadID  domain.ThreadID  `json:"thread_id,omitempty"`
	TurnID    domain.TurnID    `json:"turn_id,omitempty"`
	// TODO(P3): ItemID 与 CallID 仅为工具调用及结果配对保留；由后续 OpenSpec 同时提供真实消费者、验证与测试时启用，否则删除。
	ItemID  domain.ItemID   `json:"item_id,omitempty"`
	CallID  domain.CallID   `json:"call_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Validate 按当前协议版本校验事件 kind 与强类型 payload。
// Provider 阶段的语义事件尚未绑定 Session identity，因此 identity 由宿主边界另行校验。
func (event Event) Validate() error {
	if event.Timestamp.IsZero() {
		return fmt.Errorf("runtime event timestamp is invalid")
	}
	if event.ItemID != "" || event.CallID != "" {
		return fmt.Errorf("runtime event reserved identity is invalid")
	}
	hasIdentity := event.SessionID != "" || event.ThreadID != "" || event.TurnID != ""
	if hasIdentity && (!event.SessionID.Valid() || !event.ThreadID.Valid() || !event.TurnID.Valid()) {
		return fmt.Errorf("runtime event identity is invalid")
	}
	switch event.Kind {
	case EventTurnStarted:
		return ValidateTurnStarted(event)
	case EventAssistantTextDelta:
		_, err := DecodeAssistantTextDelta(event)
		return err
	case EventTurnCompleted:
		_, err := DecodeTurnCompleted(event)
		return err
	case EventTurnFailed:
		_, err := DecodeTurnFailed(event)
		return err
	default:
		return fmt.Errorf("runtime event kind is invalid")
	}
}

func newEvent(kind EventKind) Event {
	return Event{
		Version:   CurrentVersion,
		Kind:      kind,
		Timestamp: time.Now().UTC(),
	}
}

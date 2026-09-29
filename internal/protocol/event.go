// Package protocol 定义 Runtime 与宿主之间可版本化的 command/event 协议。
package protocol

import (
	"encoding/json"
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
	ItemID    domain.ItemID    `json:"item_id,omitempty"`
	CallID    domain.CallID    `json:"call_id,omitempty"`
	Payload   json.RawMessage  `json:"payload,omitempty"`
}

func newEvent(kind EventKind) Event {
	return Event{
		Version:   CurrentVersion,
		Kind:      kind,
		Timestamp: time.Now().UTC(),
	}
}

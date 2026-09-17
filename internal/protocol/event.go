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
	EventSessionStarted         EventKind = "session_started"
	EventTurnStarted            EventKind = "turn_started"
	EventAssistantItemStarted   EventKind = "assistant_item_started"
	EventAssistantTextDelta     EventKind = "assistant_text_delta"
	EventReasoningStarted       EventKind = "reasoning_started"
	EventReasoningDelta         EventKind = "reasoning_delta"
	EventReasoningSectionBreak  EventKind = "reasoning_section_break"
	EventToolCallStarted        EventKind = "tool_call_started"
	EventToolInputDelta         EventKind = "tool_input_delta"
	EventToolCallReady          EventKind = "tool_call_ready"
	EventPatchDraftUpdated      EventKind = "patch_draft_updated"
	EventToolExecutionStarted   EventKind = "tool_execution_started"
	EventToolProgress           EventKind = "tool_progress"
	EventToolExecutionCompleted EventKind = "tool_execution_completed"
	EventUsageUpdated           EventKind = "usage_updated"
	EventContextCompacted       EventKind = "context_compacted"
	EventTurnCompleted          EventKind = "turn_completed"
	EventTurnFailed             EventKind = "turn_failed"
)

// Event 是 TUI、headless、Session 和 telemetry 共同消费的语义事件信封。
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

// NewEvent 创建带当前协议版本和 UTC 时间的事件。
func NewEvent(kind EventKind) Event {
	return Event{
		Version:   CurrentVersion,
		Kind:      kind,
		Timestamp: time.Now().UTC(),
	}
}

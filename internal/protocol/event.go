// Package protocol 定义 Runtime 与宿主之间的强类型 command/event 协议。
package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"easycode/internal/domain"
)

// EventKind 描述共享语义事件类型，不包含 provider wire 细节。
type EventKind string

const (
	EventContextSnapshot       EventKind = "context_snapshot"
	EventCompactionStarted     EventKind = "compaction_started"
	EventCompactionCompleted   EventKind = "compaction_completed"
	EventCompactionFailed      EventKind = "compaction_failed"
	EventTranscriptCommitted   EventKind = "transcript_committed"
	EventUserInputCommitted    EventKind = "user_input_committed"
	EventTurnStarted           EventKind = "turn_started"
	EventAssistantTextDelta    EventKind = "assistant_text_delta"
	EventTurnCompleted         EventKind = "turn_completed"
	EventTurnFailed            EventKind = "turn_failed"
	EventToolApprovalRequested EventKind = "tool_approval_requested"
	EventToolApprovalResolved  EventKind = "tool_approval_resolved"
	EventToolReady             EventKind = "tool_ready"
	EventToolStarted           EventKind = "tool_started"
	EventToolProgress          EventKind = "tool_progress"
	EventToolTerminal          EventKind = "tool_terminal"
)

// Event 是 TUI、headless 和其他宿主共同消费的进程内语义事件信封。
type Event struct {
	CompactionID domain.CompactionID `json:"compaction_id,omitempty"`
	Kind         EventKind           `json:"kind"`
	Timestamp    time.Time           `json:"timestamp"`
	SessionID    domain.SessionID    `json:"session_id,omitempty"`
	ThreadID     domain.ThreadID     `json:"thread_id,omitempty"`
	TurnID       domain.TurnID       `json:"turn_id,omitempty"`
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
	if event.Kind == EventContextSnapshot || event.Kind == EventCompactionStarted || event.Kind == EventCompactionCompleted || event.Kind == EventCompactionFailed {
		if !event.SessionID.Valid() || !event.ThreadID.Valid() || event.TurnID != "" {
			return fmt.Errorf("context event scope is invalid")
		}
		if event.Kind == EventContextSnapshot {
			if event.CompactionID != "" {
				return fmt.Errorf("query event has compaction identity")
			}
			_, err := DecodeContextSnapshot(event)
			return err
		}
		if !event.CompactionID.Valid() {
			return fmt.Errorf("compaction event identity is invalid")
		}
		switch event.Kind {
		case EventCompactionStarted:
			_, err := DecodeCompactionStarted(event)
			return err
		case EventCompactionCompleted:
			_, err := DecodeCompactionCompleted(event)
			return err
		case EventCompactionFailed:
			_, err := DecodeCompactionFailed(event)
			return err
		}
	}
	if event.CompactionID != "" {
		return fmt.Errorf("turn event contains compaction identity")
	}
	hasIdentity := event.SessionID != "" || event.ThreadID != "" || event.TurnID != ""
	if hasIdentity && (!event.SessionID.Valid() || !event.ThreadID.Valid() || !event.TurnID.Valid()) {
		return fmt.Errorf("runtime event identity is invalid")
	}
	switch event.Kind {
	case EventTranscriptCommitted:
		if !hasIdentity {
			return fmt.Errorf("transcript event identity is required")
		}
		_, err := DecodeTranscriptCommitted(event)
		return err
	case EventUserInputCommitted:
		_, err := DecodeUserInputCommitted(event)
		return err
	case EventToolReady, EventToolStarted, EventToolProgress, EventToolTerminal:
		if !hasIdentity {
			return fmt.Errorf("command event identity is required")
		}
		_, err := DecodeToolActivity(event)
		return err
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
	case EventToolApprovalRequested:
		_, err := DecodeToolApprovalRequested(event)
		return err
	case EventToolApprovalResolved:
		_, err := DecodeToolApprovalResolved(event)
		return err
	default:
		return fmt.Errorf("runtime event kind is invalid")
	}
}

func newEvent(kind EventKind) Event {
	return Event{
		Kind:      kind,
		Timestamp: time.Now().UTC(),
	}
}

package headless

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// StreamContextSnapshotEvent 是只读查询的外部 v1 数值快照。
type StreamContextSnapshotEvent struct {
	Version   int                            `json:"version"`
	Type      string                         `json:"type"`
	RequestID protocol.RequestID             `json:"request_id"`
	SessionID domain.SessionID               `json:"session_id"`
	ThreadID  domain.ThreadID                `json:"thread_id"`
	Snapshot  protocol.ContextMetricsPayload `json:"snapshot"`
}

// StreamCompactionStartedEvent 表示 started 已成功 Sync。
type StreamCompactionStartedEvent struct {
	Scope           domain.CompactionScope `json:"scope"`
	Version         int                    `json:"version"`
	Type            string                 `json:"type"`
	RequestID       protocol.RequestID     `json:"request_id"`
	SessionID       domain.SessionID       `json:"session_id"`
	ThreadID        domain.ThreadID        `json:"thread_id"`
	CompactionID    domain.CompactionID    `json:"compaction_id"`
	ContextRevision uint64                 `json:"context_revision"`
}

// StreamCompactionCompletedEvent 不暴露摘要或原生 provenance。
type StreamCompactionCompletedEvent struct {
	Version      int                 `json:"version"`
	Type         string              `json:"type"`
	RequestID    protocol.RequestID  `json:"request_id"`
	SessionID    domain.SessionID    `json:"session_id"`
	ThreadID     domain.ThreadID     `json:"thread_id"`
	CompactionID domain.CompactionID `json:"compaction_id"`
	protocol.CompactionCompletedPayload
}

// StreamCompactionFailedEvent 必须带明确 durable 字段。
type StreamCompactionFailedEvent struct {
	Version      int                 `json:"version"`
	Type         string              `json:"type"`
	RequestID    protocol.RequestID  `json:"request_id"`
	SessionID    domain.SessionID    `json:"session_id"`
	ThreadID     domain.ThreadID     `json:"thread_id"`
	CompactionID domain.CompactionID `json:"compaction_id"`
	protocol.CompactionFailedPayload
}

func (StreamContextSnapshotEvent) streamEvent()     {}
func (StreamCompactionStartedEvent) streamEvent()   {}
func (StreamCompactionCompletedEvent) streamEvent() {}
func (StreamCompactionFailedEvent) streamEvent()    {}

func (projectorValue *StreamProjector) projectContext(item protocol.ControlItem) (StreamEvent, error) {
	event, _ := item.ContextEvent()
	if event.SessionID != projectorValue.sessionID || event.ThreadID != projectorValue.threadID {
		return nil, fmt.Errorf("context event identity mismatch")
	}
	if protocol.IsCompactionPresentation(event) {
		return nil, nil
	}
	switch event.Kind {
	case protocol.EventTaskProgressSnapshot:
		payload, err := protocol.DecodeTaskProgressSnapshot(event)
		if err != nil {
			return nil, err
		}
		value := StreamTaskSnapshotEvent{Version: eventVersion, Type: "tasks.snapshot", RequestID: item.RequestID(), SessionID: event.SessionID, ThreadID: event.ThreadID, Snapshot: payload}
		return value, validateTaskSnapshotEvent(value)
	case protocol.EventEffortSnapshot:
		payload, err := protocol.DecodeEffortSnapshot(event)
		if err != nil {
			return nil, err
		}
		return StreamEffortSnapshotEvent{Version: eventVersion, Type: "effort.snapshot", RequestID: item.RequestID(), SessionID: event.SessionID, ThreadID: event.ThreadID, Snapshot: newStreamEffortSnapshot(payload)}, nil
	case protocol.EventCacheSnapshot:
		payload, err := protocol.DecodeCacheSnapshot(event)
		if err != nil {
			return nil, err
		}
		return StreamCacheSnapshotEvent{Version: eventVersion, Type: "cache.snapshot", RequestID: item.RequestID(), SessionID: event.SessionID, ThreadID: event.ThreadID, Snapshot: newStreamCacheSnapshot(payload)}, nil
	case protocol.EventContextSnapshot:
		payload, err := protocol.DecodeContextSnapshot(event)
		if err != nil {
			return nil, err
		}
		return StreamContextSnapshotEvent{Version: eventVersion, Type: "context.snapshot", RequestID: item.RequestID(), SessionID: event.SessionID, ThreadID: event.ThreadID, Snapshot: payload.ContextMetricsPayload}, nil
	case protocol.EventCompactionStarted:
		payload, err := protocol.DecodeCompactionStarted(event)
		if err != nil {
			return nil, err
		}
		return StreamCompactionStartedEvent{Scope: payload.Scope, Version: eventVersion, Type: "compaction.started", RequestID: item.RequestID(), SessionID: event.SessionID, ThreadID: event.ThreadID, CompactionID: event.CompactionID, ContextRevision: payload.ContextRevision}, nil
	case protocol.EventCompactionCompleted:
		payload, err := protocol.DecodeCompactionCompleted(event)
		if err != nil {
			return nil, err
		}
		return StreamCompactionCompletedEvent{Version: eventVersion, Type: "compaction.completed", RequestID: item.RequestID(), SessionID: event.SessionID, ThreadID: event.ThreadID, CompactionID: event.CompactionID, CompactionCompletedPayload: payload}, nil
	case protocol.EventCompactionFailed:
		payload, err := protocol.DecodeCompactionFailed(event)
		if err != nil {
			return nil, err
		}
		return StreamCompactionFailedEvent{Version: eventVersion, Type: "compaction.failed", RequestID: item.RequestID(), SessionID: event.SessionID, ThreadID: event.ThreadID, CompactionID: event.CompactionID, CompactionFailedPayload: payload}, nil
	default:
		return nil, fmt.Errorf("context event kind is invalid")
	}
}

func validateCompactionWire(version int, kind, want string, requestID protocol.RequestID, sessionID domain.SessionID, threadID domain.ThreadID, id domain.CompactionID) error {
	if version != eventVersion || kind != want || !requestID.Valid() || !id.Valid() {
		return fmt.Errorf("compaction wire identity is invalid")
	}
	return validateThreadIdentity(sessionID, threadID)
}

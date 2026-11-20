package headless

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

const (
	// MaxTaskQueryBytes 限制独立任务查询的完整编码行。
	MaxTaskQueryBytes = 4 << 10
	// MaxTaskSnapshotFrameBytes 同时覆盖外部 envelope、任务资料及换行。
	MaxTaskSnapshotFrameBytes = 96 << 10
)

func decodeTaskQuery(line []byte, requestID protocol.RequestID) (protocol.Command, error) {
	if len(line) > MaxTaskQueryBytes {
		return protocol.Command{}, fmt.Errorf("tasks.query command exceeds limit")
	}
	var wire streamCommandEnvelope
	if err := codec.DecodeObject(line, &wire, "version", "type", "request_id"); err != nil {
		return protocol.Command{}, fmt.Errorf("tasks.query command is invalid")
	}
	return protocol.NewQueryTasksCommand(requestID)
}

// StreamTaskSnapshotEvent 只响应显式查询，不承载 Provider 输入或工具 profile。
type StreamTaskSnapshotEvent struct {
	Version   int                                  `json:"version"`
	Type      string                               `json:"type"`
	RequestID protocol.RequestID                   `json:"request_id"`
	SessionID domain.SessionID                     `json:"session_id"`
	ThreadID  domain.ThreadID                      `json:"thread_id"`
	Snapshot  protocol.TaskProgressSnapshotPayload `json:"snapshot"`
}

func (StreamTaskSnapshotEvent) streamEvent() {}

func validateTaskSnapshotEvent(value StreamTaskSnapshotEvent) error {
	if value.Version != eventVersion || value.Type != "tasks.snapshot" || !value.RequestID.Valid() || validateThreadIdentity(value.SessionID, value.ThreadID) != nil || value.Snapshot.Validate() != nil {
		return fmt.Errorf("task snapshot event is invalid")
	}
	encoded, err := codec.MarshalStable(value)
	if err != nil || len(encoded)+1 > MaxTaskSnapshotFrameBytes {
		return fmt.Errorf("task snapshot event exceeds limit")
	}
	return nil
}

// DecodeTaskSnapshotEvent 严格读取唯一外部 v1 快照，不忽略未知身份或 profile。
func DecodeTaskSnapshotEvent(raw []byte) (StreamTaskSnapshotEvent, error) {
	if len(raw)+1 > MaxTaskSnapshotFrameBytes {
		return StreamTaskSnapshotEvent{}, fmt.Errorf("task snapshot event exceeds limit")
	}
	var value StreamTaskSnapshotEvent
	if err := codec.UnmarshalRecord(raw, &value); err != nil {
		return StreamTaskSnapshotEvent{}, fmt.Errorf("task snapshot encoding is invalid")
	}
	if err := validateTaskSnapshotEvent(value); err != nil {
		return StreamTaskSnapshotEvent{}, err
	}
	return value, nil
}

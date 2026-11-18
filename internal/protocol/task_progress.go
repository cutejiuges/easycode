package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// MaxTaskSnapshotBytes 限制完整任务观测编码，包含来源和宿主信封。
const MaxTaskSnapshotBytes = 96 << 10

// TaskProgressSnapshotPayload 只投影规范任务和派生计数，不携带工具来源语法。
type TaskProgressSnapshotPayload struct {
	Progress domain.TaskProgressState `json:"progress"`
	Counts   domain.TaskCounts        `json:"counts"`
}

// NewTaskProgressSnapshotPayload 从已验证状态构造完整观测。
func NewTaskProgressSnapshotPayload(state domain.TaskProgressState) (TaskProgressSnapshotPayload, error) {
	p := TaskProgressSnapshotPayload{Progress: state.Clone(), Counts: state.Counts()}
	return p, p.Validate()
}

// Validate 核对三态、来源与每一派生统计。
func (p TaskProgressSnapshotPayload) Validate() error {
	if p.Progress.Validate() != nil || p.Counts != p.Progress.Counts() {
		return fmt.Errorf("task progress snapshot is invalid")
	}
	return nil
}

// Clone 返回与原观测不共享任务切片的副本。
func (p TaskProgressSnapshotPayload) Clone() TaskProgressSnapshotPayload {
	p.Progress = p.Progress.Clone()
	return p
}

// Equal 比较全部来源、任务字段和计数，不能只按结果序号去重。
func (p TaskProgressSnapshotPayload) Equal(other TaskProgressSnapshotPayload) bool {
	return p.Counts == other.Counts && p.Progress.Equal(other.Progress)
}

// NewQueryTasksCommand 创建不包含额外参数的只读线程查询。
func NewQueryTasksCommand(requestID RequestID) (Command, error) {
	command := Command{kind: CommandQueryTasks, requestID: requestID}
	return command, command.Validate()
}

// NewTaskProgressSnapshot 创建没有伪造活动回合身份的当前线程观测。
func NewTaskProgressSnapshot(sessionID domain.SessionID, threadID domain.ThreadID, state domain.TaskProgressState) (Event, error) {
	return newTaskProgressEvent(EventTaskProgressSnapshot, sessionID, threadID, "", state)
}

// NewTaskProgressUpdated 只发布实际成功结果所属回合的当前更新。
func NewTaskProgressUpdated(sessionID domain.SessionID, threadID domain.ThreadID, turnID domain.TurnID, state domain.TaskProgressState) (Event, error) {
	return newTaskProgressEvent(EventTaskProgressUpdated, sessionID, threadID, turnID, state)
}

func newTaskProgressEvent(kind EventKind, sessionID domain.SessionID, threadID domain.ThreadID, turnID domain.TurnID, state domain.TaskProgressState) (Event, error) {
	payload, err := NewTaskProgressSnapshotPayload(state)
	if err != nil {
		return Event{}, err
	}
	raw, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, fmt.Errorf("task progress snapshot encoding failed")
	}
	event := newEvent(kind)
	event.SessionID, event.ThreadID, event.TurnID = sessionID, threadID, turnID
	event.Payload = raw
	return event, event.Validate()
}

// DecodeTaskProgressSnapshot 严格解码当前查询，业务 none 仅由合法快照表达。
func DecodeTaskProgressSnapshot(event Event) (TaskProgressSnapshotPayload, error) {
	if event.Kind != EventTaskProgressSnapshot || event.TurnID != "" {
		return TaskProgressSnapshotPayload{}, fmt.Errorf("task progress query event is invalid")
	}
	return decodeTaskProgressEvent(event)
}

// DecodeTaskProgressUpdated 核对更新来源和事件所属回合一致。
func DecodeTaskProgressUpdated(event Event) (TaskProgressSnapshotPayload, error) {
	if event.Kind != EventTaskProgressUpdated || !event.TurnID.Valid() {
		return TaskProgressSnapshotPayload{}, fmt.Errorf("task progress update event is invalid")
	}
	payload, err := decodeTaskProgressEvent(event)
	if err != nil {
		return TaskProgressSnapshotPayload{}, err
	}
	source, ok := payload.Progress.Source()
	if !ok || source.TurnID() != event.TurnID {
		return TaskProgressSnapshotPayload{}, fmt.Errorf("task progress update source is invalid")
	}
	return payload, nil
}

func decodeTaskProgressEvent(event Event) (TaskProgressSnapshotPayload, error) {
	var payload TaskProgressSnapshotPayload
	if !event.SessionID.Valid() || !event.ThreadID.Valid() || event.Timestamp.IsZero() || event.CompactionID != "" || event.ItemID != "" || event.CallID != "" {
		return payload, fmt.Errorf("task progress event identity is invalid")
	}
	if codec.ValidateBoundedJSON(event.Payload, MaxTaskSnapshotBytes, 8, true) != nil || codec.UnmarshalRecord(event.Payload, &payload) != nil || payload.Validate() != nil {
		return TaskProgressSnapshotPayload{}, fmt.Errorf("task progress snapshot encoding is invalid")
	}
	encoded, err := codec.MarshalStable(event)
	if err != nil || len(encoded) > MaxTaskSnapshotBytes {
		return TaskProgressSnapshotPayload{}, fmt.Errorf("task progress event encoding exceeds limit")
	}
	return payload, nil
}

// NewCorrelatedTaskProgressItem 仅接受任务查询观测，不能混入其他上下文事件。
func NewCorrelatedTaskProgressItem(requestID RequestID, event Event) (ControlItem, error) {
	if event.Kind != EventTaskProgressSnapshot {
		return ControlItem{}, fmt.Errorf("task progress query event kind is invalid")
	}
	return NewCorrelatedContextItem(requestID, event)
}

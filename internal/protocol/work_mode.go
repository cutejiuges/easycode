package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const maxWorkModeSnapshotBytes = 64

// NewSetWorkModeCommand 创建仅供交互宿主显式设置目标模式的命令。
func NewSetWorkModeCommand(requestID RequestID, mode domain.WorkMode) (Command, error) {
	command := Command{kind: CommandSetWorkMode, requestID: requestID, workMode: mode}
	if err := command.Validate(); err != nil {
		return Command{}, err
	}
	return command, nil
}

// WorkMode 返回设置命令的目标；其他命令返回零值。
func (command Command) WorkMode() domain.WorkMode { return command.workMode }

// WorkModeSnapshotPayload 是控制 owner 关联当前交互会话的已确认模式。
type WorkModeSnapshotPayload struct {
	Mode domain.WorkMode `json:"mode"`
}

// Validate 拒绝未设置或未知的工作模式。
func (payload WorkModeSnapshotPayload) Validate() error { return payload.Mode.Validate() }

// NewWorkModeSnapshot 投影一次已接纳的模式控制结果。
func NewWorkModeSnapshot(sessionID domain.SessionID, threadID domain.ThreadID, mode domain.WorkMode) (Event, error) {
	raw, err := codec.MarshalStable(WorkModeSnapshotPayload{Mode: mode})
	if err != nil {
		return Event{}, err
	}
	return newScopedPayloadEvent(EventWorkModeSnapshot, sessionID, threadID, "", "", raw)
}

// DecodeWorkModeSnapshot 严格读取有界模式确认。
func DecodeWorkModeSnapshot(event Event) (WorkModeSnapshotPayload, error) {
	var payload WorkModeSnapshotPayload
	if event.Kind != EventWorkModeSnapshot || len(event.Payload) > maxWorkModeSnapshotBytes {
		return payload, fmt.Errorf("work mode snapshot is invalid")
	}
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return payload, err
	}
	return payload, payload.Validate()
}

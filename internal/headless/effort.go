package headless

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// MaxEffortCommandBytes 限制独立控制行，既有正文行上限不变。
const MaxEffortCommandBytes = 4 << 10

type streamSetEffortCommand struct {
	Version   int                `json:"version"`
	Type      string             `json:"type"`
	RequestID string             `json:"request_id"`
	Mode      domain.EffortMode  `json:"mode"`
	Level     domain.EffortLevel `json:"level,omitempty"`
}

func decodeEffortCommand(line []byte, kind protocol.CommandKind, requestID protocol.RequestID) (protocol.Command, error) {
	if len(line) > MaxEffortCommandBytes {
		return protocol.Command{}, fmt.Errorf("effort command exceeds limit")
	}
	if kind == protocol.CommandQueryEffort {
		var wire streamCommandEnvelope
		if err := codec.DecodeObject(line, &wire, "version", "type", "request_id"); err != nil {
			return protocol.Command{}, fmt.Errorf("effort.query command is invalid")
		}
		return protocol.NewQueryEffortCommand(requestID)
	}
	var wire streamSetEffortCommand
	if err := codec.DecodeObject(line, &wire, "version", "type", "request_id", "mode"); err != nil {
		return protocol.Command{}, fmt.Errorf("effort.set command is invalid")
	}
	members, err := codec.ObjectMembers(line)
	if err != nil {
		return protocol.Command{}, fmt.Errorf("effort.set command is invalid")
	}
	_, hasLevel := members["level"]
	var choice domain.EffortChoice
	switch wire.Mode {
	case domain.EffortAuto:
		choice = domain.NewAutomaticEffortChoice()
	case domain.EffortReset:
		choice = domain.NewResetEffortChoice()
	case domain.EffortValue:
		choice, err = domain.NewExplicitEffortChoice(wire.Level)
	default:
		return protocol.Command{}, fmt.Errorf("effort.set mode is invalid")
	}
	if err != nil || hasLevel != (wire.Mode == domain.EffortValue) {
		return protocol.Command{}, fmt.Errorf("effort.set command is invalid")
	}
	return protocol.NewSetEffortCommand(requestID, choice)
}

// StreamEffortSnapshot 是外部 v1 的显式安全投影，不嵌入内部 context 结构。
type StreamEffortSnapshot struct {
	Scope             string                        `json:"scope"`
	Family            domain.ProviderFamily         `json:"family"`
	Startup           protocol.EffortValuePayload   `json:"startup"`
	Selected          protocol.EffortValuePayload   `json:"selected"`
	Source            string                        `json:"source"`
	SelectionRevision uint64                        `json:"selection_revision"`
	Activity          string                        `json:"activity"`
	Active            *protocol.EffortActivePayload `json:"active,omitempty"`
	Pending           bool                          `json:"pending"`
	AllowedLevels     []domain.EffortLevel          `json:"allowed_levels"`
}

func newStreamEffortSnapshot(p protocol.EffortSnapshotPayload) StreamEffortSnapshot {
	return StreamEffortSnapshot{Scope: p.Scope, Family: p.Family, Startup: p.Startup, Selected: p.Selected, Source: p.Source, SelectionRevision: p.SelectionRevision, Activity: p.Activity, Active: p.Active, Pending: p.Pending, AllowedLevels: append([]domain.EffortLevel{}, p.AllowedLevels...)}
}
func (p StreamEffortSnapshot) validate() error {
	value := protocol.EffortSnapshotPayload{Scope: p.Scope, Family: p.Family, Startup: p.Startup, Selected: p.Selected, Source: p.Source, SelectionRevision: p.SelectionRevision, Activity: p.Activity, Active: p.Active, Pending: p.Pending, AllowedLevels: p.AllowedLevels}
	if err := value.Validate(); err != nil {
		return err
	}
	raw, err := codec.MarshalStable(p)
	if err != nil {
		return err
	}
	if len(raw) > protocol.MaxEffortSnapshotBytes {
		return fmt.Errorf("effort snapshot exceeds limit")
	}
	return nil
}

// StreamEffortSnapshotEvent 只在显式查询或成功设置后输出。
type StreamEffortSnapshotEvent struct {
	Version   int                  `json:"version"`
	Type      string               `json:"type"`
	RequestID protocol.RequestID   `json:"request_id"`
	SessionID domain.SessionID     `json:"session_id"`
	ThreadID  domain.ThreadID      `json:"thread_id"`
	Snapshot  StreamEffortSnapshot `json:"snapshot"`
}

func (StreamEffortSnapshotEvent) streamEvent() {}

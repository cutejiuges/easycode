package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// MaxEffortSnapshotBytes 限制进程设置观测，不包含任何原生历史。
const MaxEffortSnapshotBytes = 16 << 10

// EffortValuePayload 区分字段省略与原生显式档位。
type EffortValuePayload struct {
	State string             `json:"state"`
	Value domain.EffortLevel `json:"value,omitempty"`
}

// NewEffortValuePayload 投影操作设置，不推断服务端默认值。
func NewEffortValuePayload(settings domain.InferenceSettings) (EffortValuePayload, error) {
	if err := settings.Validate(); err != nil {
		return EffortValuePayload{}, err
	}
	value := EffortValuePayload{State: "omitted"}
	if level := settings.Effort(); level != "" {
		value.State = "explicit"
		value.Value = level
	}
	return value, nil
}

// Validate 验证唯一字段组合以及原生 family 枚举。
func (value EffortValuePayload) Validate(family domain.ProviderFamily) error {
	levels, err := domain.EffortLevels(family)
	if err != nil {
		return err
	}
	if value.State == "omitted" && value.Value == "" {
		return nil
	}
	if value.State == "explicit" {
		for _, level := range levels {
			if value.Value == level {
				return nil
			}
		}
	}
	return fmt.Errorf("effort value is invalid")
}

func (value EffortValuePayload) validateShape() error {
	if value.State == "omitted" && value.Value == "" || value.State == "explicit" && value.Value.Valid() {
		return nil
	}
	return fmt.Errorf("effort value is invalid")
}

// UnmarshalJSON 严格区分省略分支和显式分支，拒绝空值替代省略。
func (value *EffortValuePayload) UnmarshalJSON(raw []byte) error {
	type wire EffortValuePayload
	var decoded wire
	if err := codec.DecodeObject(raw, &decoded, "state"); err != nil {
		return err
	}
	members, err := codec.ObjectMembers(raw)
	if err != nil {
		return err
	}
	_, hasValue := members["value"]
	if decoded.State == "omitted" && hasValue || decoded.State == "explicit" && (!hasValue || decoded.Value == "") || decoded.State != "omitted" && decoded.State != "explicit" {
		return fmt.Errorf("effort value shape is invalid")
	}
	candidate := EffortValuePayload(decoded)
	if err := candidate.validateShape(); err != nil {
		return err
	}
	*value = candidate
	return nil
}

// EffortActivePayload 是活动操作捕获值的封闭联合；身份只由专属构造器设置。
type EffortActivePayload struct{ wire effortActiveWire }

type effortActiveWire struct {
	Kind              string              `json:"kind"`
	Phase             string              `json:"phase"`
	Effort            EffortValuePayload  `json:"effort"`
	SelectionRevision uint64              `json:"selection_revision"`
	CancelRequested   bool                `json:"cancel_requested"`
	RequestID         RequestID           `json:"request_id,omitempty"`
	TurnID            domain.TurnID       `json:"turn_id,omitempty"`
	CompactionID      domain.CompactionID `json:"compaction_id,omitempty"`
}

// NewStartingEffortActive 使用原输入身份表示尚未发布 durable started 的回合。
func NewStartingEffortActive(requestID RequestID, phase string, effort EffortValuePayload, revision uint64, cancelled bool) (EffortActivePayload, error) {
	value := EffortActivePayload{effortActiveWire{Kind: "turn_starting", Phase: phase, Effort: effort, SelectionRevision: revision, CancelRequested: cancelled, RequestID: requestID}}
	return value, value.validateShape()
}

// NewTurnEffortActive 只绑定已 durable 发布的回合身份。
func NewTurnEffortActive(turnID domain.TurnID, phase string, effort EffortValuePayload, revision uint64, cancelled bool) (EffortActivePayload, error) {
	value := EffortActivePayload{effortActiveWire{Kind: "turn", Phase: phase, Effort: effort, SelectionRevision: revision, CancelRequested: cancelled, TurnID: turnID}}
	return value, value.validateShape()
}

// NewCompactionEffortActive 只绑定独立手动摘要身份。
func NewCompactionEffortActive(id domain.CompactionID, phase string, effort EffortValuePayload, revision uint64, cancelled bool) (EffortActivePayload, error) {
	value := EffortActivePayload{effortActiveWire{Kind: "manual_compaction", Phase: phase, Effort: effort, SelectionRevision: revision, CancelRequested: cancelled, CompactionID: id}}
	return value, value.validateShape()
}

func (value EffortActivePayload) validateIdentity() error {
	w := value.wire
	switch w.Kind {
	case "turn_starting":
		if !w.RequestID.Valid() || w.TurnID != "" || w.CompactionID != "" || w.Phase != "starting" && w.Phase != "finishing" {
			return fmt.Errorf("starting effort identity is invalid")
		}
	case "turn":
		if !w.TurnID.Valid() || w.RequestID != "" || w.CompactionID != "" || w.Phase != "running" && w.Phase != "finishing" {
			return fmt.Errorf("turn effort identity is invalid")
		}
	case "manual_compaction":
		if !w.CompactionID.Valid() || w.RequestID != "" || w.TurnID != "" || w.Phase != "starting" && w.Phase != "running" && w.Phase != "finishing" {
			return fmt.Errorf("compaction effort identity is invalid")
		}
	default:
		return fmt.Errorf("active effort kind is invalid")
	}
	return nil
}

// Validate 校验捕获值和活动身份。
func (value EffortActivePayload) Validate(family domain.ProviderFamily) error {
	if err := value.validateIdentity(); err != nil {
		return err
	}
	return value.wire.Effort.Validate(family)
}

func (value EffortActivePayload) validateShape() error {
	if err := value.validateIdentity(); err != nil {
		return err
	}
	return value.wire.Effort.validateShape()
}

// MarshalJSON 仅编码当前分支的身份。
func (value EffortActivePayload) MarshalJSON() ([]byte, error) {
	return codec.MarshalStable(value.wire)
}

// UnmarshalJSON 拒绝混合身份，即使额外身份是空字符串也失败。
func (value *EffortActivePayload) UnmarshalJSON(raw []byte) error {
	var w effortActiveWire
	if err := codec.DecodeObject(raw, &w, "kind", "phase", "effort", "selection_revision", "cancel_requested"); err != nil {
		return err
	}
	members, err := codec.ObjectMembers(raw)
	if err != nil {
		return err
	}
	expected := ""
	switch w.Kind {
	case "turn_starting":
		expected = "request_id"
	case "turn":
		expected = "turn_id"
	case "manual_compaction":
		expected = "compaction_id"
	}
	for _, field := range []string{"request_id", "turn_id", "compaction_id"} {
		_, exists := members[field]
		if exists != (field == expected) {
			return fmt.Errorf("active effort identity shape is invalid")
		}
	}
	candidate := EffortActivePayload{wire: w}
	if err := candidate.validateShape(); err != nil {
		return err
	}
	*value = candidate
	return nil
}

// Kind 返回封闭活动类型。
func (value EffortActivePayload) Kind() string { return value.wire.Kind }

// Phase 返回 starting、running 或 finishing。
func (value EffortActivePayload) Phase() string { return value.wire.Phase }

// Effort 返回捕获的 wire 值。
func (value EffortActivePayload) Effort() EffortValuePayload { return value.wire.Effort }

// SelectionRevision 返回捕获时的进程序号。
func (value EffortActivePayload) SelectionRevision() uint64 { return value.wire.SelectionRevision }

// CancelRequested 表示取消已接收，不表示清理完成。
func (value EffortActivePayload) CancelRequested() bool { return value.wire.CancelRequested }

// RequestID 返回 starting 分支的输入身份。
func (value EffortActivePayload) RequestID() RequestID { return value.wire.RequestID }

// TurnID 返回 durable turn 分支的身份。
func (value EffortActivePayload) TurnID() domain.TurnID { return value.wire.TurnID }

// CompactionID 返回独立摘要身份。
func (value EffortActivePayload) CompactionID() domain.CompactionID { return value.wire.CompactionID }

// EffortSnapshotPayload 描述本进程的选择及完整操作捕获值，不证明请求已经发送。
type EffortSnapshotPayload struct {
	Scope             string                `json:"scope"`
	Family            domain.ProviderFamily `json:"family"`
	Startup           EffortValuePayload    `json:"startup"`
	Selected          EffortValuePayload    `json:"selected"`
	Source            string                `json:"source"`
	SelectionRevision uint64                `json:"selection_revision"`
	Activity          string                `json:"activity"`
	Active            *EffortActivePayload  `json:"active,omitempty"`
	Pending           bool                  `json:"pending"`
	AllowedLevels     []domain.EffortLevel  `json:"allowed_levels"`
}

// Validate 验证字段组合、捕获序号及有界原生档位序列。
func (value EffortSnapshotPayload) Validate() error {
	if value.Scope != "process" || value.Startup.Validate(value.Family) != nil || value.Selected.Validate(value.Family) != nil {
		return fmt.Errorf("effort snapshot scope or value is invalid")
	}
	switch value.Source {
	case "startup":
		if value.Selected != value.Startup {
			return fmt.Errorf("startup effort differs from selection")
		}
	case "runtime":
		if value.Selected.State != "explicit" {
			return fmt.Errorf("runtime effort is not explicit")
		}
	case "auto":
		if value.Selected.State != "omitted" {
			return fmt.Errorf("automatic effort is not omitted")
		}
	default:
		return fmt.Errorf("effort source is invalid")
	}
	if value.SelectionRevision == 0 && value.Source != "startup" {
		return fmt.Errorf("initial effort source is invalid")
	}
	if value.Activity == "idle" {
		if value.Active != nil || value.Pending {
			return fmt.Errorf("idle effort has active state")
		}
	} else {
		if value.Activity != "turn" && value.Activity != "compacting" || value.Active == nil {
			return fmt.Errorf("effort activity is invalid")
		}
		if err := value.Active.Validate(value.Family); err != nil {
			return err
		}
		if value.Active.Kind() == "turn_starting" && value.Activity != "turn" || value.Active.Kind() == "manual_compaction" && value.Activity != "compacting" || value.Active.SelectionRevision() > value.SelectionRevision || value.Pending != (value.Active.Effort() != value.Selected) {
			return fmt.Errorf("effort capture is inconsistent")
		}
		if value.Active.SelectionRevision() == value.SelectionRevision && value.Pending {
			return fmt.Errorf("same-revision effort capture differs from selection")
		}
	}
	levels, _ := domain.EffortLevels(value.Family)
	if value.AllowedLevels == nil || len(value.AllowedLevels) > len(levels) {
		return fmt.Errorf("allowed effort levels are invalid")
	}
	cursor := 0
	for _, allowed := range value.AllowedLevels {
		for cursor < len(levels) && levels[cursor] != allowed {
			cursor++
		}
		if cursor == len(levels) {
			return fmt.Errorf("allowed effort levels are unordered")
		}
		cursor++
	}
	return nil
}

// NewEffortSnapshot 创建有界、thread 关联的进程设置观测。
func NewEffortSnapshot(sessionID domain.SessionID, threadID domain.ThreadID, payload EffortSnapshotPayload) (Event, error) {
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	raw, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, err
	}
	if len(raw) > MaxEffortSnapshotBytes {
		return Event{}, fmt.Errorf("effort snapshot exceeds limit")
	}
	return newScopedPayloadEvent(EventEffortSnapshot, sessionID, threadID, "", "", raw)
}

// DecodeEffortSnapshot 严格解码唯一当前观测结构。
func DecodeEffortSnapshot(event Event) (EffortSnapshotPayload, error) {
	var payload EffortSnapshotPayload
	if event.Kind != EventEffortSnapshot || len(event.Payload) > MaxEffortSnapshotBytes {
		return payload, fmt.Errorf("effort snapshot is invalid")
	}
	if err := codec.ValidateBoundedJSON(event.Payload, MaxEffortSnapshotBytes, 8, true); err != nil {
		return payload, err
	}
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return payload, err
	}
	return payload, payload.Validate()
}

// NewCorrelatedEffortItem 仅接受设置观测事件，防止错误策略返回其他查询分支。
func NewCorrelatedEffortItem(requestID RequestID, event Event) (ControlItem, error) {
	if event.Kind != EventEffortSnapshot {
		return ControlItem{}, fmt.Errorf("effort event kind is invalid")
	}
	return NewCorrelatedContextItem(requestID, event)
}

package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"easycode/internal/codec"
)

// MaxTaskProgressBytes 为规范更新之外的固定来源信封保留有界空间。
const MaxTaskProgressBytes = MaxTaskUpdateBytes + 1024

// TaskProgressSource 指向成功结果，不使用 ready 或详情序号替代。
type TaskProgressSource struct {
	resultSequence uint64
	turnID         TurnID
	invocationID   string
}

type taskProgressSourceWire struct {
	ResultSequence uint64 `json:"result_sequence"`
	TurnID         TurnID `json:"turn_id"`
	InvocationID   string `json:"invocation_id"`
}

// NewTaskProgressSource 纯内存校验实际结果来源，不能证明该记录已持久化。
func NewTaskProgressSource(resultSequence uint64, turnID TurnID, invocationID string) (TaskProgressSource, error) {
	v := TaskProgressSource{resultSequence, turnID, invocationID}
	if err := v.Validate(); err != nil {
		return TaskProgressSource{}, err
	}
	return v, nil
}

// ResultSequence 返回 journal 成功结果位置，不是格式版本。
func (v TaskProgressSource) ResultSequence() uint64 { return v.resultSequence }

// TurnID 返回实际提交该结果的回合。
func (v TaskProgressSource) TurnID() TurnID { return v.turnID }

// InvocationID 返回原工具调用的 canonical UUIDv7 文本。
func (v TaskProgressSource) InvocationID() string { return v.invocationID }

// Validate 校验来源身份，真正成功事实由 Session 和 Runtime 核验。
func (v TaskProgressSource) Validate() error {
	if v.resultSequence == 0 || !v.turnID.Valid() {
		return fmt.Errorf("task progress source is invalid")
	}
	if _, err := parseUUIDv7(v.invocationID); err != nil {
		return fmt.Errorf("task progress invocation is invalid")
	}
	return nil
}

// MarshalJSON 只保存完整的当前来源字段。
func (v TaskProgressSource) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(taskProgressSourceWire{v.resultSequence, v.turnID, v.invocationID})
}

// DecodeTaskProgressSource 拒绝缺失、空值和未知来源字段。
func DecodeTaskProgressSource(raw []byte) (TaskProgressSource, error) {
	if codec.ValidateBoundedJSON(raw, 1024, 2, true) != nil {
		return TaskProgressSource{}, fmt.Errorf("task progress source encoding is invalid")
	}
	var w taskProgressSourceWire
	if codec.DecodeObject(raw, &w, "result_sequence", "turn_id", "invocation_id") != nil {
		return TaskProgressSource{}, fmt.Errorf("task progress source encoding is invalid")
	}
	return NewTaskProgressSource(w.ResultSequence, w.TurnID, w.InvocationID)
}

// UnmarshalJSON 使嵌套来源执行同一严格校验。
func (v *TaskProgressSource) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeTaskProgressSource(raw)
	if err != nil {
		return err
	}
	*v = decoded
	return nil
}

// TaskProgressKind 区分尚无任务事实、明确清除及非空清单。
type TaskProgressKind string

const (
	TaskProgressNone     TaskProgressKind = "none"
	TaskProgressClear    TaskProgressKind = "clear"
	TaskProgressSnapshot TaskProgressKind = "snapshot"
)

// TaskProgressState 是当前线程的不可变任务事实投影。
type TaskProgressState struct {
	kind   TaskProgressKind
	source TaskProgressSource
	update TaskUpdate
}

type taskProgressWire struct {
	State  TaskProgressKind    `json:"state"`
	Source *TaskProgressSource `json:"source,omitempty"`
	Update *TaskUpdate         `json:"update,omitempty"`
}

// NewNoTaskProgress 显式构造从未成功更新的当前分支。
func NewNoTaskProgress() TaskProgressState { return TaskProgressState{kind: TaskProgressNone} }

// NewTaskProgressState 依据完整更新决定 clear 或 snapshot，不接受任意分支名。
func NewTaskProgressState(source TaskProgressSource, update TaskUpdate) (TaskProgressState, error) {
	kind := TaskProgressSnapshot
	if len(update.items) == 0 {
		kind = TaskProgressClear
	}
	v := TaskProgressState{kind: kind, source: source, update: update.Clone()}
	if err := v.Validate(); err != nil {
		return TaskProgressState{}, err
	}
	return v, nil
}

// Kind 返回唯一当前分支。
func (v TaskProgressState) Kind() TaskProgressKind { return v.kind }

// IsZero 标识尚未构造的值，不能作为业务 none 写入记录。
func (v TaskProgressState) IsZero() bool {
	return v.kind == "" && v.source == (TaskProgressSource{}) && v.update.IsZero()
}

// Source 仅为成功更新分支返回完整来源。
func (v TaskProgressState) Source() (TaskProgressSource, bool) {
	return v.source, v.kind == TaskProgressClear || v.kind == TaskProgressSnapshot
}

// Update 返回与当前状态不共享可变切片的完整规范更新。
func (v TaskProgressState) Update() (TaskUpdate, bool) {
	return v.update.Clone(), v.kind == TaskProgressClear || v.kind == TaskProgressSnapshot
}

// Counts 返回当前清单统计，none 和 clear 均为零。
func (v TaskProgressState) Counts() TaskCounts { return v.update.Counts() }

// Clone 深拷贝当前完整更新。
func (v TaskProgressState) Clone() TaskProgressState {
	v.update = v.update.Clone()
	return v
}

// Equal 同时比较分支、实际来源和全部规范内容。
func (v TaskProgressState) Equal(other TaskProgressState) bool {
	return v.kind == other.kind && v.source == other.source && v.update.Equal(other.update)
}

// Validate 禁止缺少来源、混合分支或以零值代替明确 none。
func (v TaskProgressState) Validate() error {
	switch v.kind {
	case TaskProgressNone:
		if v.source != (TaskProgressSource{}) || !v.update.IsZero() {
			return fmt.Errorf("empty task progress has unexpected fields")
		}
	case TaskProgressClear, TaskProgressSnapshot:
		if v.source.Validate() != nil || v.update.Validate() != nil || (v.kind == TaskProgressClear) != (len(v.update.items) == 0) {
			return fmt.Errorf("task progress state is invalid")
		}
	default:
		return fmt.Errorf("task progress state kind is invalid")
	}
	return nil
}

// MarshalJSON 保持 none 无来源，而另外两种分支字段完整。
func (v TaskProgressState) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	w := taskProgressWire{State: v.kind}
	if v.kind != TaskProgressNone {
		source, update := v.source, v.update.Clone()
		w.Source, w.Update = &source, &update
	}
	raw, err := codec.MarshalStable(w)
	if err != nil || len(raw) > MaxTaskProgressBytes {
		return nil, fmt.Errorf("task progress encoding exceeds limit")
	}
	return raw, nil
}

// CanonicalJSON 返回独立且确定的完整状态编码。
func (v TaskProgressState) CanonicalJSON() []byte {
	raw, _ := v.MarshalJSON()
	return raw
}

// DecodeTaskProgressState 严格解码三种当前分支，不猜测旧格式。
func DecodeTaskProgressState(raw []byte) (TaskProgressState, error) {
	if codec.ValidateBoundedJSON(raw, MaxTaskProgressBytes, 6, true) != nil {
		return TaskProgressState{}, fmt.Errorf("task progress encoding is invalid")
	}
	var w taskProgressWire
	if codec.DecodeObject(raw, &w, "state") != nil {
		return TaskProgressState{}, fmt.Errorf("task progress encoding is invalid")
	}
	if w.State == TaskProgressNone {
		if w.Source != nil || w.Update != nil {
			return TaskProgressState{}, fmt.Errorf("empty task progress has unexpected fields")
		}
		return NewNoTaskProgress(), nil
	}
	if w.Source == nil || w.Update == nil {
		return TaskProgressState{}, fmt.Errorf("task progress fields are missing")
	}
	v := TaskProgressState{kind: w.State, source: *w.Source, update: w.Update.Clone()}
	return v, v.Validate()
}

// UnmarshalJSON 保持 checkpoint 等嵌套对象的三态字段校验。
func (v *TaskProgressState) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeTaskProgressState(raw)
	if err != nil {
		return err
	}
	*v = decoded
	return nil
}

// ContentStamp 只标识任务内容，排除来源身份；none 不产生内容 stamp。
func (v TaskProgressState) ContentStamp() string {
	if v.Validate() != nil || v.kind == TaskProgressNone {
		return ""
	}
	type contentWire struct {
		State  TaskProgressKind `json:"state"`
		Update TaskUpdate       `json:"update"`
	}
	raw, err := codec.MarshalStable(contentWire{v.kind, v.update})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	// MaxTaskSteps 限制一次完整任务清单的条目数。
	MaxTaskSteps = 20
	// MaxTaskTextBytes 限制单个任务标签或进行中描述的原始 UTF-8 字节。
	MaxTaskTextBytes = 512
	// MaxTaskExplanationBytes 限制完整更新说明的原始 UTF-8 字节。
	MaxTaskExplanationBytes = 2 << 10
	// MaxTaskTotalTextBytes 同时限制所有任务文字和更新说明。
	MaxTaskTotalTextBytes = 16 << 10
	// MaxTaskUpdateBytes 限制完整规范更新的实际 JSON 编码。
	MaxTaskUpdateBytes = 64 << 10
)

// TaskStatus 只表示模型报告的进度，不证明工具执行或验证成功。
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
)

// Valid 判断状态是否属于唯一当前任务契约。
func (s TaskStatus) Valid() bool {
	return s == TaskPending || s == TaskInProgress || s == TaskCompleted
}

// TaskStep 保存规范文字；来源专属的必需字段由工具输入契约验证。
type TaskStep struct {
	content    string
	activeForm string
	status     TaskStatus
}

type taskStepWire struct {
	Content    string     `json:"content"`
	ActiveForm string     `json:"active_form"`
	Status     TaskStatus `json:"status"`
}

// NewTaskStep 纯内存构造步骤，不裁剪或规范化原始文字。
func NewTaskStep(content, activeForm string, status TaskStatus) (TaskStep, error) {
	v := TaskStep{content: content, activeForm: activeForm, status: status}
	if err := v.Validate(); err != nil {
		return TaskStep{}, err
	}
	return v, nil
}

// Content 返回未经显示转义的任务标签。
func (v TaskStep) Content() string { return v.content }

// ActiveForm 返回原进行中描述；规范值允许为空。
func (v TaskStep) ActiveForm() string { return v.activeForm }

// Status 返回模型报告的状态。
func (v TaskStep) Status() TaskStatus { return v.status }

// Validate 校验公共文字约束，不推断 Provider 或工具来源。
func (v TaskStep) Validate() error {
	if !v.status.Valid() || !validTaskText(v.content, false) || !validTaskText(v.activeForm, true) {
		return fmt.Errorf("task step is invalid")
	}
	return nil
}

func validTaskText(text string, allowEmpty bool) bool {
	if text == "" {
		return allowEmpty
	}
	if len(text) > MaxTaskTextBytes || !utf8.ValidString(text) || strings.TrimSpace(text) != text {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// MarshalJSON 仅编码经过验证的唯一规范字段。
func (v TaskStep) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(taskStepWire{v.content, v.activeForm, v.status})
}

// DecodeTaskStep 拒绝缺字段、null、重复键及未知字段。
func DecodeTaskStep(raw []byte) (TaskStep, error) {
	if err := codec.ValidateBoundedJSON(raw, MaxTaskUpdateBytes, 2, true); err != nil {
		return TaskStep{}, fmt.Errorf("task step encoding is invalid")
	}
	var w taskStepWire
	if err := codec.DecodeObject(raw, &w, "content", "active_form", "status"); err != nil {
		return TaskStep{}, fmt.Errorf("task step encoding is invalid")
	}
	return NewTaskStep(w.Content, w.ActiveForm, w.Status)
}

// UnmarshalJSON 使嵌套步骤同样执行严格当前契约。
func (v *TaskStep) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeTaskStep(raw)
	if err != nil {
		return err
	}
	*v = decoded
	return nil
}

// TaskCounts 是任务条目派生的有界统计，不代表真实执行结果。
type TaskCounts struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
}

// TaskUpdate 是完整清单替换，不携带 facade profile 或持久化来源。
type TaskUpdate struct {
	items       []TaskStep
	explanation string
}

type taskUpdateWire struct {
	Items       []TaskStep `json:"items"`
	Explanation string     `json:"explanation"`
}

// NewTaskUpdate 创建与调用方不共享切片的完整更新；空清单明确表达清除。
func NewTaskUpdate(items []TaskStep, explanation string) (TaskUpdate, error) {
	v := TaskUpdate{items: append(make([]TaskStep, 0, len(items)), items...), explanation: explanation}
	if err := v.Validate(); err != nil {
		return TaskUpdate{}, err
	}
	return v, nil
}

// Items 返回独立步骤切片，保留清单顺序。
func (v TaskUpdate) Items() []TaskStep { return slices.Clone(v.items) }

// Explanation 返回未经显示转义的完整说明。
func (v TaskUpdate) Explanation() string { return v.explanation }

// IsZero 区分未构造值和合法的显式空清单。
func (v TaskUpdate) IsZero() bool { return v.items == nil && v.explanation == "" }

// Clone 复制可变切片，所有文字和状态均为值语义。
func (v TaskUpdate) Clone() TaskUpdate {
	v.items = slices.Clone(v.items)
	return v
}

// Equal 比较全部规范字段，不进行 Unicode 归一化或来源推断。
func (v TaskUpdate) Equal(other TaskUpdate) bool {
	return (v.items == nil) == (other.items == nil) && v.explanation == other.explanation && slices.Equal(v.items, other.items)
}

// Counts 从真实条目派生统计，不保存另一份可变状态。
func (v TaskUpdate) Counts() TaskCounts {
	c := TaskCounts{Total: len(v.items)}
	for _, item := range v.items {
		switch item.status {
		case TaskPending:
			c.Pending++
		case TaskInProgress:
			c.InProgress++
		case TaskCompleted:
			c.Completed++
		}
	}
	return c
}

func (v TaskUpdate) validateFields() error {
	if v.items == nil || len(v.items) > MaxTaskSteps || !utf8.ValidString(v.explanation) || len(v.explanation) > MaxTaskExplanationBytes {
		return fmt.Errorf("task update is invalid")
	}
	for _, r := range v.explanation {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return fmt.Errorf("task explanation is invalid")
		}
	}
	textBytes, inProgress := len(v.explanation), 0
	seen := make(map[string]struct{}, len(v.items))
	for _, item := range v.items {
		if err := item.Validate(); err != nil {
			return err
		}
		if _, exists := seen[item.content]; exists {
			return fmt.Errorf("task labels are duplicated")
		}
		seen[item.content] = struct{}{}
		textBytes += len(item.content) + len(item.activeForm)
		if item.status == TaskInProgress {
			inProgress++
		}
	}
	if inProgress > 1 || textBytes > MaxTaskTotalTextBytes {
		return fmt.Errorf("task update exceeds limits")
	}
	return nil
}

func (v TaskUpdate) encode() ([]byte, error) {
	if err := v.validateFields(); err != nil {
		return nil, err
	}
	raw, err := codec.MarshalStable(taskUpdateWire{v.items, v.explanation})
	if err != nil || len(raw) > MaxTaskUpdateBytes {
		return nil, fmt.Errorf("task update encoding exceeds limit")
	}
	return raw, nil
}

// Validate 同时验证文字预算和实际规范 JSON 编码大小。
func (v TaskUpdate) Validate() error {
	_, err := v.encode()
	return err
}

// CanonicalJSON 返回独立且确定的当前编码；未构造值返回空。
func (v TaskUpdate) CanonicalJSON() []byte {
	raw, _ := v.encode()
	return raw
}

// MarshalJSON 不编码缺失或超限的任务值。
func (v TaskUpdate) MarshalJSON() ([]byte, error) { return v.encode() }

// DecodeTaskUpdate 严格读取完整规范快照，保留原字节语义。
func DecodeTaskUpdate(raw []byte) (TaskUpdate, error) {
	if err := codec.ValidateBoundedJSON(raw, MaxTaskUpdateBytes, 4, true); err != nil {
		return TaskUpdate{}, fmt.Errorf("task update encoding is invalid")
	}
	var w taskUpdateWire
	if err := codec.DecodeObject(raw, &w, "items", "explanation"); err != nil || w.Items == nil {
		return TaskUpdate{}, fmt.Errorf("task update encoding is invalid")
	}
	return NewTaskUpdate(w.Items, w.Explanation)
}

// UnmarshalJSON 让 Session、checkpoint 和宿主嵌套值共用同一严格 decoder。
func (v *TaskUpdate) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeTaskUpdate(raw)
	if err != nil {
		return err
	}
	*v = decoded
	return nil
}

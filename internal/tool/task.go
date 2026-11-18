package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const (
	// CapabilityTaskProgress 标识仅报告进度的完整任务清单替换。
	CapabilityTaskProgress CapabilityID = "task.progress"
	// MaxTaskInputBytes 限制原始参数及当前工具输入的编码大小。
	MaxTaskInputBytes = 64 << 10
	// MaxTaskPreviewBytes 只限制英文计数确认，不限制完整任务 metadata。
	MaxTaskPreviewBytes = 1 << 10
	// 固定 profile 与字段包装不占用规范领域快照的 64 KiB 正文额度。
	maxTaskStoredInputBytes = domain.MaxTaskUpdateBytes + 128
)

// TaskProfile 约束当前 facade 的输入语法，不进入领域任务快照。
type TaskProfile string

const (
	TaskProfileTodoWrite  TaskProfile = "todo_write"
	TaskProfileUpdatePlan TaskProfile = "update_plan"
)

func (p TaskProfile) Valid() bool { return p == TaskProfileTodoWrite || p == TaskProfileUpdatePlan }

// TaskInput 保存来源约束及不含 Provider 语法的规范任务更新。
type TaskInput struct {
	profile TaskProfile
	update  domain.TaskUpdate
}

func NewTaskInput(profile TaskProfile, update domain.TaskUpdate) (TaskInput, error) {
	v := TaskInput{profile: profile, update: update.Clone()}
	return v, v.Validate()
}
func (v TaskInput) Profile() TaskProfile      { return v.profile }
func (v TaskInput) Update() domain.TaskUpdate { return v.update.Clone() }
func (v TaskInput) IsZero() bool              { return v.profile == "" && v.update.IsZero() }
func (v TaskInput) Clone() TaskInput          { v.update = v.update.Clone(); return v }
func (v TaskInput) Equal(other TaskInput) bool {
	return v.profile == other.profile && v.update.Equal(other.update)
}
func (v TaskInput) Validate() error {
	if !v.profile.Valid() || v.update.Validate() != nil {
		return errors.New("invalid task input")
	}
	if v.profile == TaskProfileTodoWrite && v.update.Explanation() != "" {
		return errors.New("TodoWrite does not accept an explanation")
	}
	for _, step := range v.update.Items() {
		if v.profile == TaskProfileTodoWrite && step.ActiveForm() == "" || v.profile == TaskProfileUpdatePlan && step.ActiveForm() != "" {
			return errors.New("task input does not match facade profile")
		}
	}
	return nil
}

type taskInputWire struct {
	Profile TaskProfile       `json:"profile"`
	Update  domain.TaskUpdate `json:"update"`
}

func (v TaskInput) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	encoded, err := codec.MarshalCanonical(taskInputWire{v.profile, v.update}, maxTaskStoredInputBytes)
	return encoded.Bytes(), err
}
func DecodeTaskInput(data []byte) (TaskInput, error) {
	if err := codec.ValidateBoundedJSON(data, maxTaskStoredInputBytes, 8, true); err != nil {
		return TaskInput{}, errors.New("invalid task input encoding")
	}
	var wire taskInputWire
	if err := codec.DecodeObject(data, &wire, "profile", "update"); err != nil {
		return TaskInput{}, errors.New("invalid task input encoding")
	}
	return NewTaskInput(wire.Profile, wire.Update)
}
func (v *TaskInput) UnmarshalJSON(data []byte) error {
	decoded, err := DecodeTaskInput(data)
	if err != nil {
		return err
	}
	*v = decoded
	return nil
}

// DecodeTodoWriteInput 只接受已显式选中的 Anthropic 参数，不猜测 JSON 来源。
func DecodeTodoWriteInput(data []byte) (TaskInput, error) {
	if err := codec.ValidateBoundedJSON(data, MaxTaskInputBytes, 5, true); err != nil {
		return TaskInput{}, errors.New("invalid TodoWrite input")
	}
	var wire struct {
		Todos []json.RawMessage `json:"todos"`
	}
	if err := codec.DecodeObject(data, &wire, "todos"); err != nil || len(wire.Todos) > domain.MaxTaskSteps {
		return TaskInput{}, errors.New("invalid TodoWrite input")
	}
	steps := make([]domain.TaskStep, 0, len(wire.Todos))
	for _, raw := range wire.Todos {
		var item struct {
			Content    string            `json:"content"`
			ActiveForm string            `json:"activeForm"`
			Status     domain.TaskStatus `json:"status"`
		}
		if err := codec.DecodeObject(raw, &item, "content", "activeForm", "status"); err != nil || item.ActiveForm == "" {
			return TaskInput{}, errors.New("invalid TodoWrite item")
		}
		step, err := domain.NewTaskStep(item.Content, item.ActiveForm, item.Status)
		if err != nil {
			return TaskInput{}, errors.New("invalid TodoWrite item")
		}
		steps = append(steps, step)
	}
	update, err := domain.NewTaskUpdate(steps, "")
	if err != nil {
		return TaskInput{}, errors.New("invalid TodoWrite update")
	}
	return NewTaskInput(TaskProfileTodoWrite, update)
}

// DecodeUpdatePlanInput 保留说明可省略的当前输入，null 仍拒绝。
func DecodeUpdatePlanInput(data []byte) (TaskInput, error) {
	if err := codec.ValidateBoundedJSON(data, MaxTaskInputBytes, 5, true); err != nil {
		return TaskInput{}, errors.New("invalid update_plan input")
	}
	var wire struct {
		Explanation string            `json:"explanation"`
		Plan        []json.RawMessage `json:"plan"`
	}
	if err := codec.DecodeObject(data, &wire, "plan"); err != nil || len(wire.Plan) > domain.MaxTaskSteps {
		return TaskInput{}, errors.New("invalid update_plan input")
	}
	steps := make([]domain.TaskStep, 0, len(wire.Plan))
	for _, raw := range wire.Plan {
		var item struct {
			Step   string            `json:"step"`
			Status domain.TaskStatus `json:"status"`
		}
		if err := codec.DecodeObject(raw, &item, "step", "status"); err != nil {
			return TaskInput{}, errors.New("invalid update_plan item")
		}
		step, err := domain.NewTaskStep(item.Step, "", item.Status)
		if err != nil {
			return TaskInput{}, errors.New("invalid update_plan item")
		}
		steps = append(steps, step)
	}
	update, err := domain.NewTaskUpdate(steps, wire.Explanation)
	if err != nil {
		return TaskInput{}, errors.New("invalid update_plan update")
	}
	return NewTaskInput(TaskProfileUpdatePlan, update)
}

// TaskInvocation 绑定已持久化的完整任务输入和唯一调用身份。
type TaskInvocation struct {
	id    InvocationID
	call  ProviderCallID
	input TaskInput
}

func NewTaskReadyCall(id ProviderCallID, input TaskInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: id, capability: CapabilityTaskProgress, wireKind: WireStructured, taskInput: input.Clone()})
}
func (c ReadyCall) TaskInput() TaskInput { return c.taskInput.Clone() }
func NewTaskInvocation(id InvocationID, call ReadyCall) (TaskInvocation, error) {
	v := TaskInvocation{id, call.providerCallID, call.taskInput.Clone()}
	if call.capability != CapabilityTaskProgress || call.Validate() != nil || v.Validate() != nil {
		return TaskInvocation{}, errors.New("invalid task invocation")
	}
	return v, nil
}
func (v TaskInvocation) InvocationID() InvocationID     { return v.id }
func (v TaskInvocation) ProviderCallID() ProviderCallID { return v.call }
func (v TaskInvocation) Input() TaskInput               { return v.input.Clone() }
func (v TaskInvocation) Validate() error {
	if !v.id.Valid() || !v.call.Valid() || v.input.Validate() != nil {
		return errors.New("invalid task invocation")
	}
	return nil
}
func (v TaskInvocation) zero() bool            { return v.id == "" && v.call == "" && v.input.IsZero() }
func (v TaskInvocation) clone() TaskInvocation { v.input = v.input.Clone(); return v }
func (v Invocation) Task() (TaskInvocation, bool) {
	return v.task.clone(), v.capability == CapabilityTaskProgress
}

// TaskExecutor 只构造冻结候选，不持有当前线程的任务状态。
type TaskExecutor interface {
	Execute(context.Context, TaskInvocation) InvocationResult
}

func taskConfirmation(update domain.TaskUpdate) string {
	counts := update.Counts()
	if counts.Total == 0 {
		return "Task list cleared"
	}
	return fmt.Sprintf("Task progress updated: %d/%d completed", counts.Completed, counts.Total)
}

func NewTaskInvocationResult(v TaskInvocation) (InvocationResult, error) {
	if err := v.Validate(); err != nil {
		return InvocationResult{}, err
	}
	p, _ := NewModelPreview(taskConfirmation(v.input.update))
	return RestoreTaskInvocationResult(v, ResultSuccess, "ok", p, v.input.update)
}

// RestoreTaskInvocationResult 只恢复完整事实；失败不得携带已应用更新。
func RestoreTaskInvocationResult(v TaskInvocation, status ResultStatus, code string, preview ModelPreview, applied domain.TaskUpdate) (InvocationResult, error) {
	if err := v.Validate(); err != nil {
		return InvocationResult{}, err
	}
	r := InvocationResult{invocationID: v.id, providerCallID: v.call, capability: CapabilityTaskProgress, wireKind: WireStructured, status: status, code: code, preview: preview, taskInput: v.input.Clone(), taskUpdate: applied.Clone()}
	return initializeResultBudget(r)
}
func (r InvocationResult) TaskInput() TaskInput { return r.taskInput.Clone() }
func (r InvocationResult) TaskUpdate() (domain.TaskUpdate, bool) {
	return r.taskUpdate.Clone(), r.capability == CapabilityTaskProgress && r.status == ResultSuccess
}
func (r InvocationResult) validateTaskResult() error {
	if r.wireKind != WireStructured || r.taskInput.Validate() != nil || r.budget.allowance > MaxTaskPreviewBytes || len(r.preview.text) > MaxTaskPreviewBytes || r.readMetadata != (ReadResultMetadata{}) || !r.globMetadata.zero() || !r.grepMetadata.zero() || !r.patchMetadata.zero() {
		return errors.New("invalid task result metadata")
	}
	if r.status == ResultSuccess {
		if !r.taskUpdate.Equal(r.taskInput.update) || r.taskUpdate.Validate() != nil || r.preview.text != taskConfirmation(r.taskUpdate) {
			return errors.New("task result does not match its input")
		}
	} else if !r.taskUpdate.IsZero() {
		return errors.New("unsuccessful task result contains applied state")
	}
	return nil
}

func newTaskErrorResult(v TaskInvocation, status ResultStatus, code, message string) InvocationResult {
	p, err := NewModelPreview(message)
	if err != nil || status == ResultSuccess || len(message) > MaxTaskPreviewBytes {
		return InvocationResult{}
	}
	r, _ := RestoreTaskInvocationResult(v, status, code, p, domain.TaskUpdate{})
	return r
}

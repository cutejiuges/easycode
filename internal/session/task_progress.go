package session

import (
	"fmt"
	"time"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func encodeTaskResult(result tool.InvocationResult) *TaskResultPayload {
	m := &TaskResultPayload{Input: result.TaskInput()}
	if update, applied := result.TaskUpdate(); applied {
		m.Update = &update
	}
	return m
}

func validateTaskResultPayload(p ToolCallResultPayload) error {
	if p.TaskMetadata == nil || p.Capability != tool.CapabilityTaskProgress || p.WireKind != tool.WireStructured || p.QuestionMetadata != nil || p.SkillMetadata != nil || p.ReadArtifactMetadata != nil || p.ReadMetadata != nil || p.GlobMetadata != nil || p.GrepMetadata != nil || p.PatchMetadata != nil || p.WriteMetadata != nil || p.ExecMetadata != nil {
		return fmt.Errorf("invalid task result union")
	}
	inv, err := validationInvocation(p)
	if err != nil {
		return err
	}
	_, err = decodeResultPayload(p, inv)
	return err
}

func decodeTaskResult(p ToolCallResultPayload, inv tool.Invocation, preview tool.ModelPreview) (tool.InvocationResult, error) {
	if p.TaskMetadata == nil || p.QuestionMetadata != nil || p.SkillMetadata != nil || p.ReadArtifactMetadata != nil || p.ReadMetadata != nil || p.GlobMetadata != nil || p.GrepMetadata != nil || p.PatchMetadata != nil || p.WriteMetadata != nil || p.ExecMetadata != nil {
		return tool.InvocationResult{}, fmt.Errorf("invalid task result union")
	}
	v, ok := inv.Task()
	if !ok || !v.Input().Equal(p.TaskMetadata.Input) {
		return tool.InvocationResult{}, fmt.Errorf("task result input mismatch")
	}
	var applied domain.TaskUpdate
	if p.TaskMetadata.Update != nil {
		applied = p.TaskMetadata.Update.Clone()
	}
	return tool.RestoreTaskInvocationResult(v, p.Status, p.Code, preview, applied)
}

// PreflightTaskResult 纯内存检查真实结果在最大信封宽度下仍可编码。
func PreflightTaskResult(identity Identity, turnID domain.TurnID, result tool.InvocationResult) error {
	if result.Capability() != tool.CapabilityTaskProgress {
		return fmt.Errorf("task result is required")
	}
	draft, err := NewToolCallResultDraft(turnID, result)
	if err != nil {
		return err
	}
	record, err := BuildRecord(identity, draft, ^uint64(0), time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), ^uint64(0), 0, 1)
	if err != nil {
		return err
	}
	_, _, err = EncodeRecord(record)
	return err
}

// replayedTaskProgress 只从通过当前语法验证的成功 result 向前推进。
// 序号或计量错误保留到读取任务证据时报告，避免抢占语法和 artifact 错误。
type replayedTaskProgress struct {
	state domain.TaskProgressState
	err   error
}

func (progress *replayedTaskProgress) accept(call ReplayedToolCall) {
	if progress.err != nil || call.Result.Capability() != tool.CapabilityTaskProgress || call.Result.Status() != tool.ResultSuccess {
		return
	}
	if call.StartedSequence == 0 || call.ResultSequence <= max(call.ReadySequence, call.StartedSequence) || tool.ValidateResultInput(call.Invocation, call.Result) != nil {
		progress.err = fmt.Errorf("invalid task result history source")
		return
	}
	progress.advance(call.ResultSequence, call.Origin.TurnID, call.Result)
}

func (progress *replayedTaskProgress) advance(sequence uint64, turn domain.TurnID, result tool.InvocationResult) {
	if previous, exists := progress.state.Source(); exists && sequence <= previous.ResultSequence() {
		progress.err = fmt.Errorf("invalid task result history source")
		return
	}
	update, ok := result.TaskUpdate()
	if !ok {
		progress.err = fmt.Errorf("successful task result has no update")
		return
	}
	source, err := domain.NewTaskProgressSource(sequence, turn, string(result.InvocationID()))
	if err != nil {
		progress.err = err
		return
	}
	progress.state, progress.err = domain.NewTaskProgressState(source, update)
}

func (progress replayedTaskProgress) snapshot() (domain.TaskProgressState, error) {
	if progress.err != nil {
		return domain.TaskProgressState{}, progress.err
	}
	return progress.state.Clone(), nil
}

// TaskProgress 返回严格回放最后一个成功结果的任务快照。
func (plan ReplayPlan) TaskProgress() (domain.TaskProgressState, error) {
	return plan.taskProgress.snapshot()
}

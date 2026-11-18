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

// TaskProgressBefore 从已验证工具事实中归约指定前缀的最近成功更新。
// before 为零时读取整个历史；实际来源始终取 result record，不能使用 ready 序号。
func TaskProgressBefore(entries []ToolHistoryEntry, before uint64) (domain.TaskProgressState, error) {
	state := domain.NewNoTaskProgress()
	var last uint64
	for _, entry := range entries {
		if !entry.HasResult || entry.Result.Capability() != tool.CapabilityTaskProgress || entry.Result.Status() != tool.ResultSuccess || before != 0 && entry.ResultSequence >= before {
			continue
		}
		evidence, evidenceErr := entry.InputResultEvidence()
		if evidenceErr != nil || evidence.ValidateResult(entry.Result) != nil || entry.Origin.TurnID != entry.TurnID || entry.Origin.SampleIndex != entry.SampleIndex || entry.Origin.CallIndex != entry.CallIndex || entry.Admission.InvocationID() != entry.Invocation.InvocationID() || entry.ResultSequence <= max(entry.Sequence, entry.Admission.Sequence()) || entry.ResultSequence <= last || tool.ValidateFrozenResult(entry.Result) != nil || tool.ValidateResultInput(entry.Invocation, entry.Result) != nil {
			return domain.TaskProgressState{}, fmt.Errorf("invalid task result history source")
		}
		update, ok := entry.Result.TaskUpdate()
		if !ok {
			return domain.TaskProgressState{}, fmt.Errorf("successful task result has no update")
		}
		source, err := domain.NewTaskProgressSource(entry.ResultSequence, entry.TurnID, string(entry.Invocation.InvocationID()))
		if err != nil {
			return domain.TaskProgressState{}, err
		}
		state, err = domain.NewTaskProgressState(source, update)
		if err != nil {
			return domain.TaskProgressState{}, err
		}
		last = entry.ResultSequence
	}
	return state, nil
}

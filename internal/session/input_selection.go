package session

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// InputSelection 只消费已经验证的 ready/gate，不读取脚本或工作区。
func (call ReplayedToolCall) InputSelection() (tool.InputSelection, error) {
	if call.Hooks == nil {
		return tool.NewUnhookedInputSelection(call.Origin, call.Invocation)
	}
	g := call.Hooks
	if err := g.Validate(call.Invocation); err != nil {
		return tool.InputSelection{}, err
	}
	if g.Decision == nil {
		return tool.InputSelection{}, fmt.Errorf("hook input has no final selection")
	}
	if g.Decision.Status == domain.HookContinue {
		return tool.NewSelectedInputSelection(call.Origin, call.Invocation, g.Plan, g.Finished, *g.Decision)
	}
	return tool.NewRejectedInputSelection(call.Origin, call.Invocation, g.Plan, g.Finished, *g.Decision)
}
func (call ReplayedToolCall) ResultInvocation() (tool.Invocation, error) {
	selection, err := call.InputSelection()
	if err != nil {
		return tool.Invocation{}, err
	}
	return selection.ResultInvocation()
}
func (call ReplayedToolCall) ExecutionAdmission() (tool.ExecutionAdmission, error) {
	if call.StartedSequence == 0 {
		return tool.ExecutionAdmission{}, nil
	}
	return tool.NewExecutionAdmission(call.Invocation.InvocationID(), call.StartedSequence)
}

// InputResultEvidence 由真实 started 与当前预算来源决定模式，正文标签不参与选择。
func (call ReplayedToolCall) InputResultEvidence() (tool.InputResultEvidence, error) {
	selection, err := call.InputSelection()
	if err != nil {
		return tool.InputResultEvidence{}, err
	}
	admission, err := call.ExecutionAdmission()
	if err != nil {
		return tool.InputResultEvidence{}, err
	}
	origin := tool.InputResultExecution
	if call.Result.Budget().Reason() == tool.BudgetRecovery {
		origin = tool.InputResultRecovery
	}
	evidence, err := tool.NewInputResultEvidence(selection, admission, origin)
	if err != nil {
		return tool.InputResultEvidence{}, err
	}
	return evidence, evidence.ValidateResult(call.Result)
}

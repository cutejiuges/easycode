package session

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// InputSelection 只消费已经验证的 ready/gate，不读取脚本或工作区。
func (call ReplayedToolCall) InputSelection() (tool.InputSelection, error) {
	return SelectToolInput(call.Origin, call.Invocation, call.Hooks)
}

// SelectToolInput 从真实调用与 Hook 事实派生当前输入选择。
func SelectToolInput(origin tool.ToolCallOrigin, invocation tool.Invocation, gate *ReplayedHookGate) (tool.InputSelection, error) {
	if gate == nil {
		return tool.NewUnhookedInputSelection(origin, invocation)
	}
	if err := gate.Validate(invocation); err != nil {
		return tool.InputSelection{}, err
	}
	if gate.Decision == nil {
		return tool.InputSelection{}, fmt.Errorf("hook input has no final selection")
	}
	if gate.Decision.Status == domain.HookContinue {
		return tool.NewSelectedInputSelection(origin, invocation, gate.Plan, gate.Finished, *gate.Decision)
	}
	return tool.NewRejectedInputSelection(origin, invocation, gate.Plan, gate.Finished, *gate.Decision)
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
	return inputResultEvidence(selection, admission, call.Result)
}

func inputResultEvidence(selection tool.InputSelection, admission tool.ExecutionAdmission, result tool.InvocationResult) (tool.InputResultEvidence, error) {
	origin := tool.InputResultExecution
	if result.Budget().Reason() == tool.BudgetRecovery {
		origin = tool.InputResultRecovery
	}
	evidence, err := tool.NewInputResultEvidence(selection, admission, origin)
	if err != nil {
		return tool.InputResultEvidence{}, err
	}
	return evidence, evidence.ValidateResult(result)
}

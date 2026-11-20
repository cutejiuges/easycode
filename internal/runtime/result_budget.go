package runtime

import (
	"easycode/internal/tool"
	"errors"
)

func (runtime *Runtime) resultAllowance(capability tool.CapabilityID) (int, error) {
	if runtime.toolTurn.resultTicket != nil {
		if runtime.toolTurn.resultTicketCapability != capability {
			return 0, errors.New("result ticket capability mismatch")
		}
		return runtime.toolTurn.resultTicket.BaseAllowance(), nil
	}
	if runtime.toolTurn.resultBudget != nil {
		return runtime.toolTurn.resultBudget.Allowance(capability)
	}
	if capability == tool.CapabilityGlob || capability == tool.CapabilityGrep {
		return tool.MaxSearchPreviewBytes, nil
	}
	if capability == tool.CapabilityQuestion {
		return tool.MaxQuestionPreviewBytes, nil
	}
	if capability == tool.CapabilityTaskProgress {
		return tool.MaxTaskPreviewBytes, nil
	}
	return tool.MaxModelPreviewBytes, nil
}

// freezeResult 由 turn owner 在提交前执行一次；恢复只重用已有预算事实。
func (runtime *Runtime) freezeResult(result tool.InvocationResult) (tool.InvocationResult, error) {
	evidence, err := runtime.resultEvidence(result)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	return runtime.freezeResultUsingEvidence(result, evidence)
}
func (runtime *Runtime) freezeResultUsingEvidence(result tool.InvocationResult, evidence tool.InputResultEvidence) (tool.InvocationResult, error) {
	if runtime.toolTurn.resultBudget == nil {
		return tool.FreezeInputRewriteResult(result, evidence, result.Budget().Allowance(), result.Budget().Reason())
	}
	if runtime.toolTurn.resultTicket == nil {
		ticket, err := runtime.toolTurn.resultBudget.Reserve(result.Capability(), false)
		if err != nil {
			return tool.InvocationResult{}, err
		}
		runtime.toolTurn.resultTicket = ticket
		runtime.toolTurn.resultTicketInvocation = result.InvocationID()
		runtime.toolTurn.resultTicketCapability = result.Capability()
		runtime.toolTurn.resultTicketPost = false
	}
	if runtime.toolTurn.resultTicketInvocation != result.InvocationID() || runtime.toolTurn.resultTicketCapability != result.Capability() {
		return tool.InvocationResult{}, errors.New("result ticket invocation mismatch")
	}
	return tool.FreezeInputRewriteResult(result, evidence, runtime.toolTurn.resultTicket.BaseAllowance(), result.Budget().Reason())
}

func (runtime *Runtime) resultBodyAllowance(invocation tool.Invocation) (int, error) {
	allowance, err := runtime.resultAllowance(invocation.Capability())
	if err != nil {
		return 0, err
	}
	state := runtime.toolTurn.toolCalls[invocation.InvocationID()]
	if state == nil {
		return 0, errors.New("tool input has no source")
	}
	selection, err := state.selection()
	if err != nil {
		return 0, err
	}
	return tool.InputRewriteBodyAllowance(selection, allowance)
}

func (runtime *Runtime) commitResultBudget(result tool.InvocationResult) error {
	if runtime.toolTurn.resultBudget == nil {
		return nil
	}
	if runtime.toolTurn.resultTicket == nil || runtime.toolTurn.resultTicketInvocation != result.InvocationID() {
		return errors.New("missing result budget ticket")
	}
	if err := runtime.toolTurn.resultTicket.CommitBase(result.Budget()); err != nil {
		return err
	}
	if !runtime.toolTurn.resultTicketPost {
		return runtime.closeResultTicket()
	}
	return nil
}
func (runtime *Runtime) closeResultTicket() error {
	if runtime.toolTurn.resultTicket == nil {
		return nil
	}
	if err := runtime.toolTurn.resultTicket.Close(); err != nil {
		return err
	}
	runtime.toolTurn.resetResultTicket()
	return nil
}

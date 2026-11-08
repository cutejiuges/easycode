package runtime

import (
	"easycode/internal/tool"
	"errors"
)

func (runtime *Runtime) resultAllowance(capability tool.CapabilityID) (int, error) {
	if runtime.resultTicket != nil {
		if runtime.resultTicketCapability != capability {
			return 0, errors.New("result ticket capability mismatch")
		}
		return runtime.resultTicket.BaseAllowance(), nil
	}
	if runtime.resultBudget != nil {
		return runtime.resultBudget.Allowance(capability)
	}
	if capability == tool.CapabilityGlob || capability == tool.CapabilityGrep {
		return tool.MaxSearchPreviewBytes, nil
	}
	if capability == tool.CapabilityQuestion {
		return tool.MaxQuestionPreviewBytes, nil
	}
	return tool.MaxModelPreviewBytes, nil
}

// freezeResult 由 turn owner 在提交前执行一次；恢复只重用已有预算事实。
func (runtime *Runtime) freezeResult(result tool.InvocationResult) (tool.InvocationResult, error) {
	if runtime.resultBudget == nil {
		return result, result.Validate()
	}
	if runtime.resultTicket == nil {
		ticket, err := runtime.resultBudget.Reserve(result.Capability(), false)
		if err != nil {
			return tool.InvocationResult{}, err
		}
		runtime.resultTicket = ticket
		runtime.resultTicketInvocation = result.InvocationID()
		runtime.resultTicketCapability = result.Capability()
		runtime.resultTicketPost = false
	}
	if runtime.resultTicketInvocation != result.InvocationID() || runtime.resultTicketCapability != result.Capability() {
		return tool.InvocationResult{}, errors.New("result ticket invocation mismatch")
	}
	return tool.FinalizeResult(result, runtime.resultTicket.BaseAllowance(), result.Budget().Reason())
}

func (runtime *Runtime) commitResultBudget(result tool.InvocationResult) error {
	if runtime.resultBudget == nil {
		return nil
	}
	if runtime.resultTicket == nil || runtime.resultTicketInvocation != result.InvocationID() {
		return errors.New("missing result budget ticket")
	}
	if err := runtime.resultTicket.CommitBase(result.Budget()); err != nil {
		return err
	}
	if !runtime.resultTicketPost {
		return runtime.closeResultTicket()
	}
	return nil
}
func (runtime *Runtime) closeResultTicket() error {
	if runtime.resultTicket == nil {
		return nil
	}
	if err := runtime.resultTicket.Close(); err != nil {
		return err
	}
	runtime.resultTicket = nil
	runtime.resultTicketInvocation = ""
	runtime.resultTicketCapability = ""
	runtime.resultTicketPost = false
	return nil
}

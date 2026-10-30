package runtime

import "easycode/internal/tool"

func (runtime *Runtime) resultAllowance(capability tool.CapabilityID) (int, error) {
	if runtime.resultBudget != nil {
		return runtime.resultBudget.Allowance(capability)
	}
	if capability == tool.CapabilityGlob || capability == tool.CapabilityGrep {
		return tool.MaxSearchPreviewBytes, nil
	}
	return tool.MaxModelPreviewBytes, nil
}

// freezeResult 由 turn owner 在提交前执行一次；恢复只重用已有预算事实。
func (runtime *Runtime) freezeResult(result tool.InvocationResult) (tool.InvocationResult, error) {
	if runtime.resultBudget == nil {
		return result, result.Validate()
	}
	allowance, err := runtime.resultAllowance(result.Capability())
	if err != nil {
		return tool.InvocationResult{}, err
	}
	result, err = tool.FinalizeResult(result, allowance, result.Budget().Reason())
	if err != nil {
		return tool.InvocationResult{}, err
	}
	if err = runtime.resultBudget.Commit(result.Capability(), result.Budget()); err != nil {
		return tool.InvocationResult{}, err
	}
	return result, nil
}

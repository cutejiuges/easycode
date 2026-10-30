package tool

import (
	"errors"
	"fmt"
)

// FinalizeResult 对已经有界的结果应用最终额度；搜索与范围读取应先使用各自渲染策略。
// 已执行的修改和命令绝不因为显示预算而改变真实终态。
func FinalizeResult(r InvocationResult, allowance int, reason BudgetReason) (InvocationResult, error) {
	if r.Validate() != nil || allowance < SafetyResultBytes || allowance > MaxModelPreviewBytes {
		return InvocationResult{}, errors.New("invalid result finalization")
	}
	if len(r.preview.text) > allowance {
		switch r.capability {
		case CapabilityRead:
			r.status, r.code = ResultError, "result_budget_exceeded"
			r.preview = ModelPreview{text: "Read failed: result_budget_exceeded; retry with a smaller limit and explicit offset"}
			r.readMetadata.startLine, r.readMetadata.endLine = 0, 0
			r.readMetadata.reachedEOF = false
			r.readMetadata.outputTruncated = true
			reason = BudgetRejected
		case CapabilityPatch, CapabilityWrite:
			r.preview = ModelPreview{text: fmt.Sprintf("%s: %s (%s); path omitted to fit result budget", r.capability, r.status, r.code)}
			reason = BudgetShortened
		default:
			if r.status == ResultSuccess {
				return InvocationResult{}, errors.New("result requires capability-specific rendering")
			}
			r.preview = ModelPreview{text: fmt.Sprintf("%s: %s (%s)", r.capability, r.status, r.code)}
			reason = BudgetShortened
		}
	}
	metadata, err := NewResultBudgetMetadata(allowance, len(r.preview.text), reason)
	if err != nil {
		return InvocationResult{}, err
	}
	return r.WithBudget(metadata)
}

// Finalize 在不重新访问文件的前提下冻结 Read 结果，预算拒绝不得保留覆盖证据。
func (e ReadExecution) Finalize(allowance int) (ReadExecution, error) {
	if err := e.Validate(); err != nil {
		return ReadExecution{}, err
	}
	result, err := FinalizeResult(e.result, allowance, BudgetPreserved)
	if err != nil {
		return ReadExecution{}, err
	}
	observation := e.observation
	if result.Status() != ResultSuccess {
		observation = UnavailableReadObservation()
	}
	return NewReadExecution(result, observation)
}

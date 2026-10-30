package tool

import (
	"errors"
	"fmt"
)

// ReadArtifactExecution 分离有界读取材料与最后形成的模型页，不产生文件覆盖证据。
type ReadArtifactExecution struct {
	invocation ReadArtifactInvocation
	window     ArtifactWindow
	failure    InvocationResult
	failed     bool
}

// NewReadArtifactExecution 绑定已校验窗口与 ready 调用，不进行最终预算分配。
func NewReadArtifactExecution(invocation ReadArtifactInvocation, window ArtifactWindow) (ReadArtifactExecution, error) {
	if invocation.Validate() != nil || window.input != invocation.Input() || window.descriptor.Validate() != nil {
		return ReadArtifactExecution{}, errors.New("invalid artifact execution")
	}
	return ReadArtifactExecution{invocation: invocation, window: window}, nil
}

// NewReadArtifactFailure 只接收真实失败，禁止用成功页绕过有界窗口。
func NewReadArtifactFailure(result InvocationResult) (ReadArtifactExecution, error) {
	if result.Validate() != nil || result.Capability() != CapabilityReadArtifact || result.Status() == ResultSuccess {
		return ReadArtifactExecution{}, errors.New("invalid artifact execution failure")
	}
	return ReadArtifactExecution{failure: result, failed: true}, nil
}

// Finalize 在 owner 分配额度后渲染一次，原始游标依据实际消费的字节递增。
func (e ReadArtifactExecution) Finalize(allowance int) (InvocationResult, error) {
	if e.failed {
		return FinalizeResult(e.failure, allowance, BudgetPreserved)
	}
	page, err := e.window.Render(allowance)
	if err != nil {
		message := "ReadArtifact failed: invalid_range"
		var rangeErr ArtifactRangeError
		if errors.As(err, &rangeErr) {
			message += fmt.Sprintf("; minimum_limit: %d", rangeErr.MinimumLimit())
		}
		preview, _ := NewModelPreview(message)
		result, buildErr := NewReadArtifactInvocationResult(e.invocation, ResultError, "invalid_range", preview, ArtifactPage{})
		if buildErr != nil {
			return InvocationResult{}, buildErr
		}
		return FinalizeResult(result, allowance, BudgetPreserved)
	}
	preview, err := page.Preview()
	if err != nil {
		return InvocationResult{}, err
	}
	r, err := NewReadArtifactInvocationResult(e.invocation, ResultSuccess, "ok", preview, page)
	if err != nil {
		return InvocationResult{}, err
	}
	reason := BudgetPreserved
	if page.NextOffset() < min(uint64(e.window.descriptor.SavedBytes()), e.invocation.Input().Offset()+e.invocation.Input().Limit()) {
		reason = BudgetShortened
	}
	return FinalizeResult(r, allowance, reason)
}

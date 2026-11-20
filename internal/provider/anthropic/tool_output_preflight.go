package anthropic

import (
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func encodeToolResult(call NativeItem, output tool.ModelOutput) (NativeItem, error) {
	if err := nativeCallEvidence(call).ValidateModelOutput(domain.ProviderAnthropic, output); err != nil {
		return NativeItem{}, err
	}
	return encodeToolResultData(call, output.Result(), output.Text())
}

// encodeToolResultData 是纯 wire 编码，也供已验证提问事实的离线正文比对复用。
func encodeToolResultData(call NativeItem, result tool.InvocationResult, text string) (NativeItem, error) {
	const mismatch = "anthropic tool output does not match call"
	if err := nativeCallEvidence(call).ValidateFrozenResult(domain.ProviderAnthropic, result, mismatch, mismatch); err != nil {
		return NativeItem{}, err
	}
	return NativeItem{Type: blockTypeToolResult, ToolUseID: call.ID, Content: text, IsError: result.Status() != tool.ResultSuccess}, nil
}

// PrepareToolOutputPreflight 复制待配对 block，不持有或修改 Conversation。
func (c *Conversation) PrepareToolOutputPreflight(inv tool.Invocation) (provider.ToolOutputPreflight, error) {
	if c == nil || c.operations.Busy() || inv.Validate() != nil {
		return nil, fmt.Errorf("anthropic tool output preflight is unavailable")
	}
	calls, err := pendingToolUses(c.history.Entries())
	if err != nil {
		return nil, err
	}
	return provider.PreflightNativeToolOutput(calls, inv, domain.ProviderAnthropic, nativeCallEvidence, encodeToolResult)
}

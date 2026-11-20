package openai

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func encodeToolResult(call NativeItem, output tool.ModelOutput) (NativeItem, error) {
	if err := nativeCallEvidence(call).ValidateModelOutput(domain.ProviderOpenAI, output); err != nil {
		return NativeItem{}, err
	}
	return encodeToolResultData(call, output.Result(), output.Text())
}

// encodeToolResultData 是纯 wire 编码，也供已验证提问事实的离线正文比对复用。
func encodeToolResultData(call NativeItem, result tool.InvocationResult, text string) (NativeItem, error) {
	evidence := nativeCallEvidence(call)
	if err := evidence.ValidateFrozenResult(domain.ProviderOpenAI, result, "invalid OpenAI tool result", "OpenAI tool output does not match call"); err != nil {
		return NativeItem{}, err
	}
	kind := "function_call_output"
	if evidence.Wire == tool.WireCustomFreeform {
		kind = "custom_tool_call_output"
	}
	b, err := codec.MarshalStable(functionOutputPayload{Status: result.Status(), Code: result.Code(), Content: text})
	if err != nil {
		return NativeItem{}, fmt.Errorf("encode OpenAI tool output failed")
	}
	return NativeItem{Type: kind, CallID: call.CallID, Output: string(b)}, nil
}

// PrepareToolOutputPreflight 复制当前原生调用，返回不触及活动历史的单项校验策略。
func (c *Conversation) PrepareToolOutputPreflight(inv tool.Invocation) (provider.ToolOutputPreflight, error) {
	if c == nil || c.operations.Busy() || inv.Validate() != nil {
		return nil, fmt.Errorf("OpenAI tool output preflight is unavailable")
	}
	calls, err := pendingFunctionCalls(c.history.Entries())
	if err != nil {
		return nil, err
	}
	return provider.PreflightNativeToolOutput(calls, inv, domain.ProviderOpenAI, nativeCallEvidence, encodeToolResult)
}

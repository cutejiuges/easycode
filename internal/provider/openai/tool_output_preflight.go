package openai

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func encodeToolResult(call NativeItem, output tool.ModelOutput) (NativeItem, error) {
	if output.Validate() != nil {
		return NativeItem{}, fmt.Errorf("invalid model tool output")
	}
	result := output.Result()
	if err := tool.ValidateFrozenResult(result); err != nil {
		return NativeItem{}, fmt.Errorf("invalid OpenAI tool result")
	}
	wire := tool.WireStructured
	kind := "function_call_output"
	if call.Type == "custom_tool_call" {
		wire = tool.WireCustomFreeform
		kind = "custom_tool_call_output"
	}
	if string(result.ProviderCallID()) != call.CallID || result.Capability() != openAICapabilityForName(call.Name) || result.WireKind() != wire {
		return NativeItem{}, fmt.Errorf("OpenAI tool output does not match call")
	}
	if call.Name == "request_user_input" {
		input, err := tool.DecodeQuestionInput([]byte(call.Arguments))
		if err != nil || result.QuestionMetadata().Fingerprint != input.Fingerprint() {
			return NativeItem{}, fmt.Errorf("question input association mismatch")
		}
		if result.Status() == tool.ResultSuccess {
			if _, err := tool.DecodeQuestionPreview(input, []byte(result.Preview().Text())); err != nil {
				return NativeItem{}, err
			}
		}
	}
	b, err := codec.MarshalStable(functionOutputPayload{Status: result.Status(), Code: result.Code(), Content: output.Text()})
	if err != nil {
		return NativeItem{}, fmt.Errorf("encode OpenAI tool output failed")
	}
	return NativeItem{Type: kind, CallID: call.CallID, Output: string(b)}, nil
}

type toolOutputPreflight struct {
	call         NativeItem
	invocationID tool.InvocationID
}

func (p toolOutputPreflight) Validate(output tool.ModelOutput) error {
	r := output.Result()
	if r.InvocationID() != p.invocationID {
		return fmt.Errorf("tool output invocation mismatch")
	}
	item, err := encodeToolResult(p.call, output)
	if err != nil {
		return err
	}
	b, err := codec.MarshalStable(item)
	if err != nil || len(b) > provider.MaxNativeCommitBytes {
		return fmt.Errorf("tool output encoding exceeds limit")
	}
	return nil
}

// PrepareToolOutputPreflight 复制当前原生调用，返回不触及活动历史的单项校验策略。
func (c *Conversation) PrepareToolOutputPreflight(inv tool.Invocation) (provider.ToolOutputPreflight, error) {
	if c == nil || c.active.Load() || c.pending.Load() || inv.Validate() != nil {
		return nil, fmt.Errorf("OpenAI tool output preflight is unavailable")
	}
	calls, err := pendingFunctionCalls(c.history.snapshot())
	if err != nil {
		return nil, err
	}
	for _, call := range calls {
		if call.CallID == string(inv.ProviderCallID()) {
			if openAICapabilityForName(call.Name) != inv.Capability() {
				return nil, fmt.Errorf("tool capability mismatch")
			}
			if q, ok := inv.Question(); ok {
				input, err := tool.DecodeQuestionInput([]byte(call.Arguments))
				if err != nil || input != q.Input() {
					return nil, fmt.Errorf("question input mismatch")
				}
			}
			return toolOutputPreflight{call.clone(), inv.InvocationID()}, nil
		}
	}
	return nil, fmt.Errorf("no matching pending tool call")
}

package anthropic

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

func encodeToolResult(call NativeItem, output tool.ModelOutput) (NativeItem, error) {
	if output.Validate() != nil {
		return NativeItem{}, fmt.Errorf("invalid model tool output")
	}
	result := output.Result()
	original := output.Evidence().Selection().Original()
	if err := tool.ValidateNativeCallInput(original, domain.ProviderAnthropic, call.Name, tool.ProviderCallID(call.ID), tool.WireStructured, call.Input); err != nil {
		return NativeItem{}, err
	}
	return encodeToolResultData(call, result, output.Text())
}

// encodeToolResultData 是纯 wire 编码，也供已验证提问事实的离线正文比对复用。
func encodeToolResultData(call NativeItem, result tool.InvocationResult, text string) (NativeItem, error) {
	if tool.ValidateFrozenResult(result) != nil || string(result.ProviderCallID()) != call.ID || result.Capability() != anthropicCapabilityForName(call.Name) || result.WireKind() != tool.WireStructured {
		return NativeItem{}, fmt.Errorf("anthropic tool output does not match call")
	}
	if call.Name == "AskUserQuestion" {
		input, err := tool.DecodeQuestionInput(call.Input)
		if err != nil || result.QuestionMetadata().Fingerprint != input.Fingerprint() {
			return NativeItem{}, fmt.Errorf("question input association mismatch")
		}
		if result.Status() == tool.ResultSuccess {
			if _, err := tool.DecodeQuestionPreview(input, []byte(result.Preview().Text())); err != nil {
				return NativeItem{}, err
			}
		}
	}
	return NativeItem{Type: blockTypeToolResult, ToolUseID: call.ID, Content: text, IsError: result.Status() != tool.ResultSuccess}, nil
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

// PrepareToolOutputPreflight 复制待配对 block，不持有或修改 Conversation。
func (c *Conversation) PrepareToolOutputPreflight(inv tool.Invocation) (provider.ToolOutputPreflight, error) {
	if c == nil || c.active.Load() || c.pending.Load() || inv.Validate() != nil {
		return nil, fmt.Errorf("anthropic tool output preflight is unavailable")
	}
	calls, err := pendingToolUses(c.history.snapshot())
	if err != nil {
		return nil, err
	}
	for _, call := range calls {
		if call.ID == string(inv.ProviderCallID()) {
			if anthropicCapabilityForName(call.Name) != inv.Capability() {
				return nil, fmt.Errorf("tool capability mismatch")
			}
			if q, ok := inv.Question(); ok {
				input, err := tool.DecodeQuestionInput(call.Input)
				if err != nil || input != q.Input() {
					return nil, fmt.Errorf("question input mismatch")
				}
			}
			return toolOutputPreflight{call.clone(), inv.InvocationID()}, nil
		}
	}
	return nil, fmt.Errorf("no matching pending tool call")
}

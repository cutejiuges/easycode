package provider

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

// NativeToolCall 是用于核对工具事实的只读调用投影。
type NativeToolCall struct {
	Name      string
	ID        tool.ProviderCallID
	Wire      tool.ProviderWireKind
	Arguments []byte
}

func (call NativeToolCall) validateInput(invocation tool.Invocation, family domain.ProviderFamily) error {
	return tool.ValidateNativeCallInput(invocation, family, call.Name, call.ID, call.Wire, call.Arguments)
}

// ValidateNativeReadyFacts 按原生调用顺序核对已冻结的原始工具事实。
func ValidateNativeReadyFacts[C any](calls []C, ready []tool.Invocation, family domain.ProviderFamily, describe func(C) NativeToolCall) error {
	for index, input := range calls {
		if index >= len(ready) {
			return fmt.Errorf("native call has no matching ready fact")
		}
		if err := describe(input).validateInput(ready[index], family); err != nil {
			return err
		}
	}
	if len(calls) != len(ready) {
		return fmt.Errorf("ready fact has no native call")
	}
	return nil
}

// ValidateNativeRestoredResult 验证匹配的 Question 或 Task 的原生结果与其冻结事实。
func ValidateNativeRestoredResult[C any](calls, outputs []C, results []tool.InvocationResult, name, label string, describe func(C) NativeToolCall, compare func(C, C, tool.InvocationResult) bool) error {
	matched := 0
	for index, call := range calls {
		info := describe(call)
		if info.Name != name {
			continue
		}
		if index >= len(outputs) {
			return fmt.Errorf("%s output is missing", label)
		}
		found := false
		for _, result := range results {
			if result.ProviderCallID() != info.ID {
				continue
			}
			if !compare(call, outputs[index], result) {
				return fmt.Errorf("native %s output differs from committed result", label)
			}
			found = true
			matched++
			break
		}
		if !found {
			return fmt.Errorf("%s output has no committed result", label)
		}
	}
	if matched != len(results) {
		return fmt.Errorf("committed %s result has no output", label)
	}
	return nil
}

type nativeOutputPreflight[C any] struct {
	call   C
	id     tool.InvocationID
	encode func(C, tool.ModelOutput) (C, error)
}

func (p nativeOutputPreflight[C]) Validate(output tool.ModelOutput) error {
	if output.Result().InvocationID() != p.id {
		return fmt.Errorf("tool output invocation mismatch")
	}
	item, err := p.encode(p.call, output)
	if err != nil {
		return err
	}
	b, err := codec.MarshalStable(item)
	if err != nil || len(b) > MaxNativeCommitBytes {
		return fmt.Errorf("tool output encoding exceeds limit")
	}
	return nil
}

// PreflightNativeToolOutput 绑定已复制的待配对调用，不占用提交或执行状态。
func PreflightNativeToolOutput[C any](calls []C, invocation tool.Invocation, family domain.ProviderFamily, describe func(C) NativeToolCall, encode func(C, tool.ModelOutput) (C, error)) (ToolOutputPreflight, error) {
	for _, call := range calls {
		info := describe(call)
		if info.ID != invocation.ProviderCallID() {
			continue
		}
		capability, _ := tool.FacadeCapability(family, info.Name)
		if capability != invocation.Capability() {
			return nil, fmt.Errorf("tool capability mismatch")
		}
		if capability == tool.CapabilityTaskProgress {
			if err := info.validateInput(invocation, family); err != nil {
				return nil, err
			}
		}
		if question, ok := invocation.Question(); ok {
			input, err := tool.DecodeQuestionInput(info.Arguments)
			if err != nil || input != question.Input() {
				return nil, fmt.Errorf("question input mismatch")
			}
		}
		return nativeOutputPreflight[C]{call: call, id: invocation.InvocationID(), encode: encode}, nil
	}
	return nil, fmt.Errorf("no matching pending tool call")
}

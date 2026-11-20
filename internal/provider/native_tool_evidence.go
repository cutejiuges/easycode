package provider

import (
	"errors"
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

// ValidateModelOutput 核对冻结输出关联的原始工具调用。
func (call NativeToolCall) ValidateModelOutput(family domain.ProviderFamily, output tool.ModelOutput) error {
	if output.Validate() != nil {
		return fmt.Errorf("invalid model tool output")
	}
	return call.validateInput(output.Evidence().Selection().Original(), family)
}

// ValidateFrozenResult 核对结果的原生调用身份和提问或任务的额外证据。
func (call NativeToolCall) ValidateFrozenResult(family domain.ProviderFamily, result tool.InvocationResult, invalid, mismatch string) error {
	if tool.ValidateFrozenResult(result) != nil {
		return errors.New(invalid)
	}
	capability, _ := tool.FacadeCapability(family, call.Name)
	if result.ProviderCallID() != call.ID || result.Capability() != capability || result.WireKind() != call.Wire {
		return errors.New(mismatch)
	}
	switch capability {
	case tool.CapabilityTaskProgress:
		ready, err := tool.NewTaskReadyCall(result.ProviderCallID(), result.TaskInput())
		if err != nil {
			return err
		}
		invocation, err := tool.NewInvocation(result.InvocationID(), ready)
		if err != nil {
			return err
		}
		return call.validateInput(invocation, family)
	case tool.CapabilityQuestion:
		input, err := tool.DecodeQuestionInput(call.Arguments)
		if err != nil || result.QuestionMetadata().Fingerprint != input.Fingerprint() {
			return fmt.Errorf("question input association mismatch")
		}
		if result.Status() == tool.ResultSuccess {
			_, err = tool.DecodeQuestionPreview(input, []byte(result.Preview().Text()))
			return err
		}
	}
	return nil
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

// ValidateNativeRestoredResults 用已验证的原始调用比对冻结提问及任务结果。
func ValidateNativeRestoredResults[C any](calls, outputs []C, results []tool.InvocationResult, family domain.ProviderFamily, describe func(C) NativeToolCall, compare func(C, C, tool.InvocationResult) bool) error {
	byCall := make(map[tool.ProviderCallID]*tool.InvocationResult, len(results))
	var questions, tasks int
	for index := range results {
		result := &results[index]
		byCall[result.ProviderCallID()] = result
		switch result.Capability() {
		case tool.CapabilityQuestion:
			questions++
		case tool.CapabilityTaskProgress:
			tasks++
		}
	}
	match := func(capability tool.CapabilityID, label string, count int) error {
		matched := 0
		for index, call := range calls {
			info := describe(call)
			actual, _ := tool.FacadeCapability(family, info.Name)
			if actual != capability {
				continue
			}
			if index >= len(outputs) {
				return fmt.Errorf("%s output is missing", label)
			}
			result := byCall[info.ID]
			if result == nil || result.Capability() != capability {
				return fmt.Errorf("%s output has no committed result", label)
			}
			if !compare(call, outputs[index], *result) {
				return fmt.Errorf("native %s output differs from committed result", label)
			}
			matched++
		}
		if matched != count {
			return fmt.Errorf("committed %s result has no output", label)
		}
		return nil
	}
	// 保留提问先于任务的恢复错误顺序。
	if err := match(tool.CapabilityQuestion, "question", questions); err != nil {
		return err
	}
	return match(tool.CapabilityTaskProgress, "task", tasks)
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

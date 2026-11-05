package tool

import (
	"context"
	"fmt"

	"easycode/internal/protocol"
)

const CapabilityQuestion CapabilityID = "user.question"
const MaxQuestionPreviewBytes = protocol.MaxQuestionPreviewBytes

// 问题值与宿主协议共享同一当前契约，避免协议层反向依赖工具层。
type QuestionID = protocol.QuestionID
type Question = protocol.Question
type QuestionOption = protocol.QuestionOption
type QuestionInput = protocol.QuestionInput
type QuestionAnswer = protocol.QuestionAnswer
type QuestionAnswerSet = protocol.QuestionAnswerSet

func NewQuestionInput(q []Question) (QuestionInput, error) { return protocol.NewQuestionInput(q) }
func DecodeQuestionInput(data []byte) (QuestionInput, error) {
	return protocol.DecodeQuestionInput(data)
}
func NewQuestionAnswerSet(a []QuestionAnswer) (QuestionAnswerSet, error) {
	return protocol.NewQuestionAnswerSet(a)
}
func DecodeQuestionAnswerSet(data []byte) (QuestionAnswerSet, error) {
	return protocol.DecodeQuestionAnswerSet(data)
}

// QuestionInvocation 绑定已持久化的提问和工具身份。
type QuestionInvocation struct {
	id    InvocationID
	call  ProviderCallID
	input QuestionInput
}

func NewQuestionReadyCall(id ProviderCallID, input QuestionInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: id, capability: CapabilityQuestion, wireKind: WireStructured, questionInput: input})
}
func (c ReadyCall) QuestionInput() QuestionInput { return c.questionInput }
func NewQuestionInvocation(id InvocationID, c ReadyCall) (QuestionInvocation, error) {
	v := QuestionInvocation{id, c.providerCallID, c.questionInput}
	if c.capability != CapabilityQuestion || c.Validate() != nil || v.Validate() != nil {
		return QuestionInvocation{}, fmt.Errorf("invalid question invocation")
	}
	return v, nil
}
func (v QuestionInvocation) InvocationID() InvocationID     { return v.id }
func (v QuestionInvocation) ProviderCallID() ProviderCallID { return v.call }
func (v QuestionInvocation) Input() QuestionInput           { return v.input }
func (v QuestionInvocation) Validate() error {
	if !v.id.Valid() || !v.call.Valid() || v.input.Validate() != nil {
		return fmt.Errorf("invalid question invocation")
	}
	return nil
}
func (v Invocation) Question() (QuestionInvocation, bool) {
	return v.question, v.capability == CapabilityQuestion
}

// QuestionResponder 由当前 turn 提供唯一回答接收和取消边界。
type QuestionResponder interface {
	Answer(context.Context, QuestionInvocation) (QuestionAnswerSet, error)
}

// QuestionExecutor 将已接收回答转换为冻结结果，不负责持久化或展示。
type QuestionExecutor interface {
	Execute(context.Context, QuestionInvocation, QuestionResponder) InvocationResult
}

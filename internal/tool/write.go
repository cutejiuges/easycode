package tool

import (
	"context"
	"errors"
)

// WriteInvocation 绑定一次完整 Write 调用及其唯一执行身份。
type WriteInvocation struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	input          WriteInput
}

// NewWriteInvocation 从持久化 ready 构造写入执行输入。
func NewWriteInvocation(id InvocationID, call ReadyCall) (WriteInvocation, error) {
	v := WriteInvocation{invocationID: id, providerCallID: call.providerCallID, input: call.writeInput}
	if call.capability != CapabilityWrite || call.Validate() != nil || v.Validate() != nil {
		return WriteInvocation{}, errors.New("invalid Write invocation")
	}
	return v, nil
}

func (v WriteInvocation) InvocationID() InvocationID     { return v.invocationID }
func (v WriteInvocation) ProviderCallID() ProviderCallID { return v.providerCallID }
func (v WriteInvocation) Input() WriteInput              { return v.input }
func (v Invocation) Write() (WriteInvocation, bool)      { return v.write, v.capability == CapabilityWrite }

// Validate 验证调用身份与输入，不访问文件系统。
func (v WriteInvocation) Validate() error {
	if !v.invocationID.Valid() || !v.providerCallID.Valid() || v.input.Validate() != nil {
		return errors.New("invalid Write invocation")
	}
	return nil
}

// WritePreparer 在 ready 持久化后只读地冻结候选及审批展示。
type WritePreparer interface {
	Prepare(context.Context, WriteInvocation, ReadObservation) (WriteCandidate, error)
}

// WriteExecutor 在批准和执行准入持久化后提交唯一候选。
type WriteExecutor interface {
	Execute(context.Context, WriteCandidate) InvocationResult
}

package tool

import "errors"

// ExecInvocation 保存命令的全局调用身份和已经校验的输入。
type ExecInvocation struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	input          ExecInput
}

// NewExecInvocation 从完整调用事实构造命令执行输入。
func NewExecInvocation(id InvocationID, call ReadyCall) (ExecInvocation, error) {
	value := ExecInvocation{invocationID: id, providerCallID: call.providerCallID, input: call.execInput}
	if call.capability != CapabilityExec || call.Validate() != nil || value.Validate() != nil {
		return ExecInvocation{}, errors.New("exec invocation requires a valid Exec call")
	}
	return value, nil
}

func (value ExecInvocation) InvocationID() InvocationID     { return value.invocationID }
func (value ExecInvocation) ProviderCallID() ProviderCallID { return value.providerCallID }
func (value ExecInvocation) Input() ExecInput               { return value.input }

// Validate 校验调用身份及命令输入。
func (value ExecInvocation) Validate() error {
	if !value.invocationID.Valid() || !value.providerCallID.Valid() || value.input.Validate() != nil {
		return errors.New("invalid Exec invocation")
	}
	return nil
}

func (invocation Invocation) Exec() (ExecInvocation, bool) {
	return invocation.exec, invocation.capability == CapabilityExec
}

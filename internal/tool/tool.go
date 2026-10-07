// Package tool 定义与 Provider wire、Session 和宿主展示解耦的工具值契约。
package tool

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	// CapabilityRead 标识受工作区约束的文本文件读取能力。
	CapabilityRead CapabilityID = "fs.read"

	// ReadInputRevision 是首版 Read 输入契约版本。
	ReadInputRevision = "read-input.v1"
	// ReadResultCodecRevision 是首版 Read 模型结果编码版本。
	ReadResultCodecRevision = "read-result.v1"
	// ReadRendererRevision 是首版 Read 文本渲染版本。
	ReadRendererRevision = "read-renderer.v1"

	// MaxModelPreviewBytes 限制单个模型可见工具结果。
	MaxModelPreviewBytes = 256 << 10
)

// CapabilityID 是与 Provider facade 名称解耦的能力标识。
type CapabilityID string

// ParseCapabilityID 只接受当前已实现且可执行的能力。
func ParseCapabilityID(value string) (CapabilityID, error) {
	if value != string(CapabilityRead) {
		return "", fmt.Errorf("unsupported tool capability")
	}
	return CapabilityRead, nil
}

// Valid 判断能力是否由当前实现支持。
func (id CapabilityID) Valid() bool { return id == CapabilityRead }

// ProviderCallID 保存 Provider 原生调用配对标识。
type ProviderCallID string

// ParseProviderCallID 校验 Provider 调用标识。
func ParseProviderCallID(value string) (ProviderCallID, error) {
	if value == "" || len(value) > 512 || !utf8.ValidString(value) {
		return "", errors.New("invalid provider call ID")
	}
	return ProviderCallID(value), nil
}

// Valid 判断 Provider 调用标识是否合法。
func (id ProviderCallID) Valid() bool {
	_, err := ParseProviderCallID(string(id))
	return err == nil
}

// ReadyCall 是完整 sample 中已经严格解码、可持久化的 Read 调用。
type ReadyCall struct {
	providerCallID ProviderCallID
	input          ReadInput
}

// NewReadyCall 构造已经完整校验的 Read 调用。
func NewReadyCall(providerCallID ProviderCallID, input ReadInput) (ReadyCall, error) {
	call := ReadyCall{providerCallID: providerCallID, input: input}
	if err := call.Validate(); err != nil {
		return ReadyCall{}, err
	}
	return call, nil
}

// ProviderCallID 返回 Provider 原生调用标识。
func (call ReadyCall) ProviderCallID() ProviderCallID { return call.providerCallID }

// Capability 返回调用对应的已实现能力。
func (call ReadyCall) Capability() CapabilityID { return CapabilityRead }

// InputRevision 返回 typed input revision。
func (call ReadyCall) InputRevision() string { return ReadInputRevision }

// ReadInput 返回 Read 输入值副本。
func (call ReadyCall) ReadInput() ReadInput { return call.input }

// Clone 返回不共享可变状态的调用副本。
func (call ReadyCall) Clone() ReadyCall { return call }

// Validate 验证调用身份和 typed input。
func (call ReadyCall) Validate() error {
	if !call.providerCallID.Valid() {
		return errors.New("ready call has invalid provider call ID")
	}
	if err := call.input.Validate(); err != nil {
		return fmt.Errorf("ready call has invalid Read input: %w", err)
	}
	return nil
}

// ReadInvocation 是 Runtime 分配 invocation identity 后交给执行器的值。
type ReadInvocation struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	input          ReadInput
}

// NewReadInvocation 从 durable ready fact 构造执行输入。
func NewReadInvocation(invocationID InvocationID, call ReadyCall) (ReadInvocation, error) {
	invocation := ReadInvocation{
		invocationID: invocationID, providerCallID: call.providerCallID, input: call.input,
	}
	if err := invocation.Validate(); err != nil {
		return ReadInvocation{}, err
	}
	return invocation, nil
}

// InvocationID 返回 EasyCode 幂等账本标识。
func (invocation ReadInvocation) InvocationID() InvocationID { return invocation.invocationID }

// ProviderCallID 返回 Provider 原生调用标识。
func (invocation ReadInvocation) ProviderCallID() ProviderCallID { return invocation.providerCallID }

// Input 返回 Read 输入值副本。
func (invocation ReadInvocation) Input() ReadInput { return invocation.input }

// Validate 验证执行输入。
func (invocation ReadInvocation) Validate() error {
	if !invocation.invocationID.Valid() {
		return errors.New("read invocation has invalid invocation ID")
	}
	call, err := NewReadyCall(invocation.providerCallID, invocation.input)
	if err != nil {
		return err
	}
	return call.Validate()
}

// ReadExecutor 执行一个已经持久化并获准执行的 Read 调用。
type ReadExecutor interface {
	Execute(context.Context, ReadInvocation) InvocationResult
}

// PolicyDecision 是固定策略的封闭决策集合。
type PolicyDecision string

const (
	// PolicyAllow 表示允许执行已注册的 Read。
	PolicyAllow PolicyDecision = "allow"
	// PolicyDeny 表示拒绝未知或未注册能力。
	PolicyDeny PolicyDecision = "deny"
)

// ReadOnlyPolicy 只允许 catalog 中真实绑定的 fs.read 能力。
type ReadOnlyPolicy struct{}

// NewReadOnlyPolicy 创建无外部状态的固定策略。
func NewReadOnlyPolicy() ReadOnlyPolicy { return ReadOnlyPolicy{} }

// Decide 对完整 typed 调用做固定决策，不公开 ask 状态。
func (ReadOnlyPolicy) Decide(call ReadyCall) PolicyDecision {
	if call.Validate() == nil && call.Capability() == CapabilityRead {
		return PolicyAllow
	}
	return PolicyDeny
}

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
	// CapabilityGlob 标识受工作区约束的文件发现能力。
	CapabilityGlob CapabilityID = "fs.glob"
	// CapabilityGrep 标识受工作区约束的文本搜索能力。
	CapabilityGrep CapabilityID = "fs.grep"

	// MaxModelPreviewBytes 限制单个 Read 模型可见工具结果。
	MaxModelPreviewBytes = 256 << 10
	// MaxSearchPreviewBytes 限制单个搜索工具的模型可见结果。
	MaxSearchPreviewBytes = 64 << 10
)

// CapabilityID 是与 Provider facade 名称解耦的能力标识。
type CapabilityID string

// ParseCapabilityID 只接受当前已实现且可执行的能力。
func ParseCapabilityID(value string) (CapabilityID, error) {
	id := CapabilityID(value)
	if !id.Valid() {
		return "", errors.New("unsupported tool capability")
	}
	return id, nil
}

// Valid 判断能力是否由当前实现支持。
func (id CapabilityID) Valid() bool {
	switch id {
	case CapabilityRead, CapabilityGlob, CapabilityGrep:
		return true
	default:
		return false
	}
}

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

// ReadyCall 是完整 sample 中已经严格解码、可持久化的三工具封闭联合。
type ReadyCall struct {
	providerCallID ProviderCallID
	capability     CapabilityID
	readInput      ReadInput
	globInput      GlobInput
	grepInput      GrepInput
}

// NewReadReadyCall 构造已经完整校验的 Read 调用。
func NewReadReadyCall(providerCallID ProviderCallID, input ReadInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: providerCallID, capability: CapabilityRead, readInput: input})
}

// NewGlobReadyCall 构造已经完整校验的 Glob 调用。
func NewGlobReadyCall(providerCallID ProviderCallID, input GlobInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: providerCallID, capability: CapabilityGlob, globInput: input})
}

// NewGrepReadyCall 构造已经完整校验的 Grep 调用。
func NewGrepReadyCall(providerCallID ProviderCallID, input GrepInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: providerCallID, capability: CapabilityGrep, grepInput: input})
}

func newReadyCall(call ReadyCall) (ReadyCall, error) {
	if err := call.Validate(); err != nil {
		return ReadyCall{}, err
	}
	return call, nil
}

func (call ReadyCall) ProviderCallID() ProviderCallID { return call.providerCallID }
func (call ReadyCall) Capability() CapabilityID       { return call.capability }
func (call ReadyCall) ReadInput() ReadInput           { return call.readInput }
func (call ReadyCall) GlobInput() GlobInput           { return call.globInput }
func (call ReadyCall) GrepInput() GrepInput           { return call.grepInput }
func (call ReadyCall) Clone() ReadyCall               { return call }

// Validate 验证调用身份、能力标签及唯一匹配的 typed input。
func (call ReadyCall) Validate() error {
	if !call.providerCallID.Valid() {
		return errors.New("ready call has invalid provider call ID")
	}
	switch call.capability {
	case CapabilityRead:
		if call.globInput != (GlobInput{}) || call.grepInput != (GrepInput{}) || call.readInput.Validate() != nil {
			return errors.New("ready call has invalid Read payload")
		}
	case CapabilityGlob:
		if call.readInput != (ReadInput{}) || call.grepInput != (GrepInput{}) || call.globInput.Validate() != nil {
			return errors.New("ready call has invalid Glob payload")
		}
	case CapabilityGrep:
		if call.readInput != (ReadInput{}) || call.globInput != (GlobInput{}) || call.grepInput.Validate() != nil {
			return errors.New("ready call has invalid Grep payload")
		}
	default:
		return errors.New("ready call has unsupported capability")
	}
	return nil
}

// ReadInvocation 是 Runtime 分配 invocation identity 后交给 Read 执行器的值。
type ReadInvocation struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	input          ReadInput
}

// GlobInvocation 是 Runtime 分配 invocation identity 后交给 Glob 执行器的值。
type GlobInvocation struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	input          GlobInput
}

// GrepInvocation 是 Runtime 分配 invocation identity 后交给 Grep 执行器的值。
type GrepInvocation struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	input          GrepInput
}

// Invocation 是带全局 identity 的 Read/Glob/Grep 执行输入封闭联合。
type Invocation struct {
	capability CapabilityID
	read       ReadInvocation
	glob       GlobInvocation
	grep       GrepInvocation
}

// NewInvocation 从 durable ready fact 构造匹配能力的执行输入。
func NewInvocation(invocationID InvocationID, call ReadyCall) (Invocation, error) {
	switch call.Capability() {
	case CapabilityRead:
		value, err := NewReadInvocation(invocationID, call)
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{capability: CapabilityRead, read: value}, nil
	case CapabilityGlob:
		value, err := NewGlobInvocation(invocationID, call)
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{capability: CapabilityGlob, glob: value}, nil
	case CapabilityGrep:
		value, err := NewGrepInvocation(invocationID, call)
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{capability: CapabilityGrep, grep: value}, nil
	default:
		return Invocation{}, errors.New("tool invocation capability is unsupported")
	}
}

func (invocation Invocation) Capability() CapabilityID { return invocation.capability }
func (invocation Invocation) InvocationID() InvocationID {
	switch invocation.capability {
	case CapabilityRead:
		return invocation.read.invocationID
	case CapabilityGlob:
		return invocation.glob.invocationID
	case CapabilityGrep:
		return invocation.grep.invocationID
	default:
		return ""
	}
}
func (invocation Invocation) ProviderCallID() ProviderCallID {
	switch invocation.capability {
	case CapabilityRead:
		return invocation.read.providerCallID
	case CapabilityGlob:
		return invocation.glob.providerCallID
	case CapabilityGrep:
		return invocation.grep.providerCallID
	default:
		return ""
	}
}
func (invocation Invocation) Read() (ReadInvocation, bool) {
	return invocation.read, invocation.capability == CapabilityRead
}
func (invocation Invocation) Glob() (GlobInvocation, bool) {
	return invocation.glob, invocation.capability == CapabilityGlob
}
func (invocation Invocation) Grep() (GrepInvocation, bool) {
	return invocation.grep, invocation.capability == CapabilityGrep
}

// Validate 验证 capability 标签与唯一 typed invocation 一致。
func (invocation Invocation) Validate() error {
	switch invocation.capability {
	case CapabilityRead:
		if invocation.glob != (GlobInvocation{}) || invocation.grep != (GrepInvocation{}) {
			return errors.New("Read invocation contains mismatched payload")
		}
		return invocation.read.Validate()
	case CapabilityGlob:
		if invocation.read != (ReadInvocation{}) || invocation.grep != (GrepInvocation{}) {
			return errors.New("Glob invocation contains mismatched payload")
		}
		return invocation.glob.Validate()
	case CapabilityGrep:
		if invocation.read != (ReadInvocation{}) || invocation.glob != (GlobInvocation{}) {
			return errors.New("Grep invocation contains mismatched payload")
		}
		return invocation.grep.Validate()
	default:
		return errors.New("tool invocation capability is invalid")
	}
}

// NewReadInvocation 从 durable ready fact 构造 Read 执行输入。
func NewReadInvocation(invocationID InvocationID, call ReadyCall) (ReadInvocation, error) {
	invocation := ReadInvocation{invocationID: invocationID, providerCallID: call.providerCallID, input: call.readInput}
	if call.capability != CapabilityRead || call.Validate() != nil || invocation.Validate() != nil {
		return ReadInvocation{}, errors.New("read invocation requires a valid Read call")
	}
	return invocation, nil
}

// NewGlobInvocation 从 durable ready fact 构造 Glob 执行输入。
func NewGlobInvocation(invocationID InvocationID, call ReadyCall) (GlobInvocation, error) {
	invocation := GlobInvocation{invocationID: invocationID, providerCallID: call.providerCallID, input: call.globInput}
	if call.capability != CapabilityGlob || call.Validate() != nil || invocation.Validate() != nil {
		return GlobInvocation{}, errors.New("glob invocation requires a valid Glob call")
	}
	return invocation, nil
}

// NewGrepInvocation 从 durable ready fact 构造 Grep 执行输入。
func NewGrepInvocation(invocationID InvocationID, call ReadyCall) (GrepInvocation, error) {
	invocation := GrepInvocation{invocationID: invocationID, providerCallID: call.providerCallID, input: call.grepInput}
	if call.capability != CapabilityGrep || call.Validate() != nil || invocation.Validate() != nil {
		return GrepInvocation{}, errors.New("grep invocation requires a valid Grep call")
	}
	return invocation, nil
}

func (invocation ReadInvocation) InvocationID() InvocationID     { return invocation.invocationID }
func (invocation ReadInvocation) ProviderCallID() ProviderCallID { return invocation.providerCallID }
func (invocation ReadInvocation) Input() ReadInput               { return invocation.input }
func (invocation GlobInvocation) InvocationID() InvocationID     { return invocation.invocationID }
func (invocation GlobInvocation) ProviderCallID() ProviderCallID { return invocation.providerCallID }
func (invocation GlobInvocation) Input() GlobInput               { return invocation.input }
func (invocation GrepInvocation) InvocationID() InvocationID     { return invocation.invocationID }
func (invocation GrepInvocation) ProviderCallID() ProviderCallID { return invocation.providerCallID }
func (invocation GrepInvocation) Input() GrepInput               { return invocation.input }

func (invocation ReadInvocation) Validate() error {
	call, err := NewReadReadyCall(invocation.providerCallID, invocation.input)
	if !invocation.invocationID.Valid() || err != nil || call.Validate() != nil {
		return errors.New("read invocation is invalid")
	}
	return nil
}

func (invocation GlobInvocation) Validate() error {
	call, err := NewGlobReadyCall(invocation.providerCallID, invocation.input)
	if !invocation.invocationID.Valid() || err != nil || call.Validate() != nil {
		return errors.New("glob invocation is invalid")
	}
	return nil
}

func (invocation GrepInvocation) Validate() error {
	call, err := NewGrepReadyCall(invocation.providerCallID, invocation.input)
	if !invocation.invocationID.Valid() || err != nil || call.Validate() != nil {
		return errors.New("grep invocation is invalid")
	}
	return nil
}

// ReadExecutor 执行一个已经持久化并获准执行的 Read 调用。
type ReadExecutor interface {
	Execute(context.Context, ReadInvocation) InvocationResult
}

// GlobExecutor 执行一个已经持久化并获准执行的 Glob 调用。
type GlobExecutor interface {
	Execute(context.Context, GlobInvocation) InvocationResult
}

// GrepExecutor 执行一个已经持久化并获准执行的 Grep 调用。
type GrepExecutor interface {
	Execute(context.Context, GrepInvocation) InvocationResult
}

// PolicyDecision 是固定策略的封闭决策集合。
type PolicyDecision string

const (
	PolicyAllow PolicyDecision = "allow"
	PolicyDeny  PolicyDecision = "deny"
)

// ReadOnlyPolicy 只允许 catalog 中真实绑定的三种只读能力。
type ReadOnlyPolicy struct{}

func NewReadOnlyPolicy() ReadOnlyPolicy { return ReadOnlyPolicy{} }

// Decide 对完整 typed 调用做固定决策，不公开 ask 状态。
func (ReadOnlyPolicy) Decide(call ReadyCall) PolicyDecision {
	if call.Validate() == nil && call.Capability().Valid() {
		return PolicyAllow
	}
	return PolicyDeny
}

// CapabilityMismatchError 返回不泄露输入正文的稳定错误。
func CapabilityMismatchError(capability CapabilityID) error {
	return fmt.Errorf("tool capability %q does not match typed payload", capability)
}

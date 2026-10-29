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
	// CapabilityPatch 标识需要批准的单文件文本Patch能力。
	CapabilityPatch CapabilityID = "fs.patch"
	// CapabilityWrite 标识单文件创建与整文件覆盖能力。
	CapabilityWrite CapabilityID = "fs.write"
	// CapabilityExec 标识需要逐次批准的前台命令执行能力。
	CapabilityExec CapabilityID = "process.exec"

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
	case CapabilityRead, CapabilityGlob, CapabilityGrep, CapabilityPatch, CapabilityWrite, CapabilityExec, CapabilityReadArtifact:
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

// ReadyCall 是完整sample中已经严格解码、可持久化的七工具封闭联合。
type ReadyCall struct {
	artifactInput  ReadArtifactInput
	providerCallID ProviderCallID
	capability     CapabilityID
	wireKind       ProviderWireKind
	readInput      ReadInput
	globInput      GlobInput
	grepInput      GrepInput
	patchInput     PatchInput
	writeInput     WriteInput
	execInput      ExecInput
}

// NewReadReadyCall 构造已经完整校验的 Read 调用。
func NewReadReadyCall(providerCallID ProviderCallID, input ReadInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: providerCallID, capability: CapabilityRead, wireKind: WireStructured, readInput: input})
}

// NewGlobReadyCall 构造已经完整校验的 Glob 调用。
func NewGlobReadyCall(providerCallID ProviderCallID, input GlobInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: providerCallID, capability: CapabilityGlob, wireKind: WireStructured, globInput: input})
}

// NewGrepReadyCall 构造已经完整校验的 Grep 调用。
func NewGrepReadyCall(providerCallID ProviderCallID, input GrepInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: providerCallID, capability: CapabilityGrep, wireKind: WireStructured, grepInput: input})
}

// NewPatchReadyCall 构造已经完整校验且保留原生wire kind的Patch调用。
func NewPatchReadyCall(providerCallID ProviderCallID, wireKind ProviderWireKind, input PatchInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: providerCallID, capability: CapabilityPatch, wireKind: wireKind, patchInput: input.Clone()})
}

// NewWriteReadyCall 构造完整校验的 structured Write 调用。
func NewWriteReadyCall(id ProviderCallID, input WriteInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: id, capability: CapabilityWrite, wireKind: WireStructured, writeInput: input})
}
func (call ReadyCall) WriteInput() WriteInput { return call.writeInput }

func newReadyCall(call ReadyCall) (ReadyCall, error) {
	if err := call.Validate(); err != nil {
		return ReadyCall{}, err
	}
	return call, nil
}

// NewExecReadyCall 构造已经完整校验的前台命令调用。
func NewExecReadyCall(providerCallID ProviderCallID, input ExecInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: providerCallID, capability: CapabilityExec, wireKind: WireStructured, execInput: input})
}

func (call ReadyCall) ExecInput() ExecInput { return call.execInput }

func (call ReadyCall) ProviderCallID() ProviderCallID { return call.providerCallID }
func (call ReadyCall) Capability() CapabilityID       { return call.capability }
func (call ReadyCall) WireKind() ProviderWireKind     { return call.wireKind }
func (call ReadyCall) ReadInput() ReadInput           { return call.readInput }
func (call ReadyCall) GlobInput() GlobInput           { return call.globInput }
func (call ReadyCall) GrepInput() GrepInput           { return call.grepInput }
func (call ReadyCall) PatchInput() PatchInput         { return call.patchInput.Clone() }
func (call ReadyCall) Clone() ReadyCall {
	call.patchInput = call.patchInput.Clone()
	return call
}

// Validate 验证调用身份、能力标签及唯一匹配的 typed input。
func (call ReadyCall) Validate() error {
	if call.capability != CapabilityReadArtifact && call.artifactInput != (ReadArtifactInput{}) {
		return errors.New("mismatched artifact input")
	}
	if call.capability != CapabilityExec && !call.execInput.zero() {
		return errors.New("ready call contains mismatched Exec payload")
	}
	if !call.providerCallID.Valid() {
		return errors.New("ready call has invalid provider call ID")
	}
	if !call.wireKind.Valid() {
		return errors.New("ready call has invalid Provider wire kind")
	}
	if call.capability != CapabilityWrite && call.writeInput != (WriteInput{}) {
		return errors.New("ready call contains mismatched Write input")
	}
	switch call.capability {
	case CapabilityReadArtifact:
		if call.wireKind != WireStructured || call.readInput != (ReadInput{}) || call.globInput != (GlobInput{}) || call.grepInput != (GrepInput{}) || !call.patchInput.zero() || call.artifactInput.Validate() != nil {
			return errors.New("invalid ReadArtifact input")
		}
	case CapabilityExec:
		if call.wireKind != WireStructured || call.readInput != (ReadInput{}) || call.globInput != (GlobInput{}) || call.grepInput != (GrepInput{}) || !call.patchInput.zero() || call.execInput.Validate() != nil {
			return errors.New("ready call has invalid Exec payload")
		}
	case CapabilityRead:
		if call.wireKind != WireStructured || call.globInput != (GlobInput{}) || call.grepInput != (GrepInput{}) || !call.patchInput.zero() || call.readInput.Validate() != nil {
			return errors.New("ready call has invalid Read payload")
		}
	case CapabilityGlob:
		if call.wireKind != WireStructured || call.readInput != (ReadInput{}) || call.grepInput != (GrepInput{}) || !call.patchInput.zero() || call.globInput.Validate() != nil {
			return errors.New("ready call has invalid Glob payload")
		}
	case CapabilityGrep:
		if call.wireKind != WireStructured || call.readInput != (ReadInput{}) || call.globInput != (GlobInput{}) || !call.patchInput.zero() || call.grepInput.Validate() != nil {
			return errors.New("ready call has invalid Grep payload")
		}
	case CapabilityPatch:
		if call.readInput != (ReadInput{}) || call.globInput != (GlobInput{}) || call.grepInput != (GrepInput{}) || call.patchInput.Validate() != nil {
			return errors.New("ready call has invalid Patch payload")
		}
	case CapabilityWrite:
		if call.wireKind != WireStructured || call.readInput != (ReadInput{}) || call.globInput != (GlobInput{}) || call.grepInput != (GrepInput{}) || !call.patchInput.zero() || call.writeInput.Validate() != nil {
			return errors.New("ready call has invalid Write payload")
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

// PatchInvocation 是Runtime分配invocation identity后交给Patch流水线的值。
type PatchInvocation struct {
	invocationID   InvocationID
	providerCallID ProviderCallID
	wireKind       ProviderWireKind
	input          PatchInput
}

// Invocation 是带全局identity的七能力执行输入封闭联合。
type Invocation struct {
	artifact   ReadArtifactInvocation
	capability CapabilityID
	read       ReadInvocation
	glob       GlobInvocation
	grep       GrepInvocation
	patch      PatchInvocation
	write      WriteInvocation
	exec       ExecInvocation
}

// NewInvocation 从 durable ready fact 构造匹配能力的执行输入。
func NewInvocation(invocationID InvocationID, call ReadyCall) (Invocation, error) {
	switch call.Capability() {
	case CapabilityReadArtifact:
		v, err := NewReadArtifactInvocation(invocationID, call)
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{capability: CapabilityReadArtifact, artifact: v}, nil
	case CapabilityExec:
		value, err := NewExecInvocation(invocationID, call)
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{capability: CapabilityExec, exec: value}, nil
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
	case CapabilityPatch:
		value, err := NewPatchInvocation(invocationID, call)
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{capability: CapabilityPatch, patch: value}, nil
	case CapabilityWrite:
		value, err := NewWriteInvocation(invocationID, call)
		if err != nil {
			return Invocation{}, err
		}
		return Invocation{capability: CapabilityWrite, write: value}, nil
	default:
		return Invocation{}, errors.New("tool invocation capability is unsupported")
	}
}

func (invocation Invocation) Capability() CapabilityID { return invocation.capability }
func (invocation Invocation) InvocationID() InvocationID {
	switch invocation.capability {
	case CapabilityReadArtifact:
		return invocation.artifact.id
	case CapabilityExec:
		return invocation.exec.invocationID
	case CapabilityRead:
		return invocation.read.invocationID
	case CapabilityGlob:
		return invocation.glob.invocationID
	case CapabilityGrep:
		return invocation.grep.invocationID
	case CapabilityPatch:
		return invocation.patch.invocationID
	case CapabilityWrite:
		return invocation.write.invocationID
	default:
		return ""
	}
}
func (invocation Invocation) ProviderCallID() ProviderCallID {
	switch invocation.capability {
	case CapabilityReadArtifact:
		return invocation.artifact.call
	case CapabilityExec:
		return invocation.exec.providerCallID
	case CapabilityRead:
		return invocation.read.providerCallID
	case CapabilityGlob:
		return invocation.glob.providerCallID
	case CapabilityGrep:
		return invocation.grep.providerCallID
	case CapabilityPatch:
		return invocation.patch.providerCallID
	case CapabilityWrite:
		return invocation.write.providerCallID
	default:
		return ""
	}
}

// WireKind 返回原Provider调用形态。
func (invocation Invocation) WireKind() ProviderWireKind {
	if invocation.capability == CapabilityPatch {
		return invocation.patch.wireKind
	}
	if invocation.capability.Valid() {
		return WireStructured
	}
	return ""
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
func (invocation Invocation) Patch() (PatchInvocation, bool) {
	return invocation.patch.clone(), invocation.capability == CapabilityPatch
}

// Validate 验证 capability 标签与唯一 typed invocation 一致。
func (invocation Invocation) Validate() error {
	if invocation.capability != CapabilityReadArtifact && invocation.artifact != (ReadArtifactInvocation{}) {
		return errors.New("mismatched artifact invocation")
	}
	if invocation.capability != CapabilityWrite && invocation.write != (WriteInvocation{}) {
		return errors.New("invocation contains mismatched Write input")
	}
	if invocation.capability != CapabilityExec && invocation.exec != (ExecInvocation{}) {
		return errors.New("invocation contains mismatched Exec payload")
	}
	switch invocation.capability {
	case CapabilityReadArtifact:
		if invocation.read != (ReadInvocation{}) || invocation.glob != (GlobInvocation{}) || invocation.grep != (GrepInvocation{}) || !invocation.patch.zero() {
			return errors.New("mismatched artifact invocation")
		}
		return invocation.artifact.Validate()
	case CapabilityExec:
		if invocation.read != (ReadInvocation{}) || invocation.glob != (GlobInvocation{}) || invocation.grep != (GrepInvocation{}) || !invocation.patch.zero() {
			return errors.New("Exec invocation contains mismatched payload")
		}
		return invocation.exec.Validate()
	case CapabilityRead:
		if invocation.glob != (GlobInvocation{}) || invocation.grep != (GrepInvocation{}) || !invocation.patch.zero() {
			return errors.New("Read invocation contains mismatched payload")
		}
		return invocation.read.Validate()
	case CapabilityGlob:
		if invocation.read != (ReadInvocation{}) || invocation.grep != (GrepInvocation{}) || !invocation.patch.zero() {
			return errors.New("Glob invocation contains mismatched payload")
		}
		return invocation.glob.Validate()
	case CapabilityGrep:
		if invocation.read != (ReadInvocation{}) || invocation.glob != (GlobInvocation{}) || !invocation.patch.zero() {
			return errors.New("Grep invocation contains mismatched payload")
		}
		return invocation.grep.Validate()
	case CapabilityPatch:
		if invocation.read != (ReadInvocation{}) || invocation.glob != (GlobInvocation{}) || invocation.grep != (GrepInvocation{}) {
			return errors.New("Patch invocation contains mismatched payload")
		}
		return invocation.patch.Validate()
	case CapabilityWrite:
		if invocation.read != (ReadInvocation{}) || invocation.glob != (GlobInvocation{}) || invocation.grep != (GrepInvocation{}) || !invocation.patch.zero() {
			return errors.New("Write invocation contains mismatched payload")
		}
		return invocation.write.Validate()
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

// NewPatchInvocation 从durable ready fact构造Patch执行输入。
func NewPatchInvocation(invocationID InvocationID, call ReadyCall) (PatchInvocation, error) {
	invocation := PatchInvocation{
		invocationID: invocationID, providerCallID: call.providerCallID,
		wireKind: call.wireKind, input: call.patchInput.Clone(),
	}
	if call.capability != CapabilityPatch || call.Validate() != nil || invocation.Validate() != nil {
		return PatchInvocation{}, errors.New("patch invocation requires a valid Patch call")
	}
	return invocation, nil
}

func (invocation ReadInvocation) InvocationID() InvocationID      { return invocation.invocationID }
func (invocation ReadInvocation) ProviderCallID() ProviderCallID  { return invocation.providerCallID }
func (invocation ReadInvocation) Input() ReadInput                { return invocation.input }
func (invocation GlobInvocation) InvocationID() InvocationID      { return invocation.invocationID }
func (invocation GlobInvocation) ProviderCallID() ProviderCallID  { return invocation.providerCallID }
func (invocation GlobInvocation) Input() GlobInput                { return invocation.input }
func (invocation GrepInvocation) InvocationID() InvocationID      { return invocation.invocationID }
func (invocation GrepInvocation) ProviderCallID() ProviderCallID  { return invocation.providerCallID }
func (invocation GrepInvocation) Input() GrepInput                { return invocation.input }
func (invocation PatchInvocation) InvocationID() InvocationID     { return invocation.invocationID }
func (invocation PatchInvocation) ProviderCallID() ProviderCallID { return invocation.providerCallID }
func (invocation PatchInvocation) WireKind() ProviderWireKind     { return invocation.wireKind }
func (invocation PatchInvocation) Input() PatchInput              { return invocation.input.Clone() }

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

func (invocation PatchInvocation) Validate() error {
	call, err := NewPatchReadyCall(invocation.providerCallID, invocation.wireKind, invocation.input)
	if !invocation.invocationID.Valid() || err != nil || call.Validate() != nil {
		return errors.New("patch invocation is invalid")
	}
	return nil
}

func (invocation PatchInvocation) clone() PatchInvocation {
	invocation.input = invocation.input.Clone()
	return invocation
}

func (invocation PatchInvocation) zero() bool {
	return invocation.invocationID == "" && invocation.providerCallID == "" && invocation.wireKind == "" && invocation.input.zero()
}

// ReadExecutor 执行一个已经持久化并获准执行的 Read 调用。
type ReadExecutor interface {
	Execute(context.Context, ReadInvocation) ReadExecution
}

// GlobExecutor 执行一个已经持久化并获准执行的 Glob 调用。
type GlobExecutor interface {
	Execute(context.Context, GlobInvocation) InvocationResult
}

// GrepExecutor 执行一个已经持久化并获准执行的 Grep 调用。
type GrepExecutor interface {
	Execute(context.Context, GrepInvocation) InvocationResult
}

// PatchPreparer 在durable ready之后读取目标并生成零写入候选。
type PatchPreparer interface {
	Prepare(context.Context, PatchInvocation) (PatchCandidate, error)
}

// PatchExecutor 执行已经durable批准并完成执行准入的不可变候选。
type PatchExecutor interface {
	Execute(context.Context, PatchCandidate) InvocationResult
}

// ExecPreparer 纯内存冻结命令候选。
type ExecPreparer interface {
	Prepare(ExecInvocation) (ExecCandidate, error)
}

// ExecExecutor 执行已经批准的候选并发布有界瞬态进度。
type ExecExecutor interface {
	Execute(context.Context, ExecCandidate, func(ExecProgress)) InvocationResult
}

// Exclusive 标识需要等待前序调用完成的副作用能力。
func (id CapabilityID) Exclusive() bool {
	return id == CapabilityPatch || id == CapabilityWrite || id == CapabilityExec
}

// PolicyDecision 是固定策略的封闭决策集合。
type PolicyDecision string

const (
	PolicyAllow PolicyDecision = "allow"
	PolicyAsk   PolicyDecision = "ask"
	PolicyDeny  PolicyDecision = "deny"
)

// Valid 判断策略决定是否合法。
func (decision PolicyDecision) Valid() bool {
	switch decision {
	case PolicyAllow, PolicyAsk, PolicyDeny:
		return true
	default:
		return false
	}
}

// Policy 对完整typed call给出封闭准入决定。
type Policy interface {
	Decide(ReadyCall) PolicyDecision
}

// ReadOnlyPolicy 只允许 catalog 中真实绑定的三种只读能力。
type ReadOnlyPolicy struct{}

func NewReadOnlyPolicy() ReadOnlyPolicy { return ReadOnlyPolicy{} }

// Decide 对完整 typed 调用做固定决策，不公开 ask 状态。
func (ReadOnlyPolicy) Decide(call ReadyCall) PolicyDecision {
	if call.Validate() == nil && (call.Capability() == CapabilityRead || call.Capability() == CapabilityGlob || call.Capability() == CapabilityGrep || call.Capability() == CapabilityReadArtifact) {
		return PolicyAllow
	}
	return PolicyDeny
}

// ApprovalPolicy 自动允许只读调用，并要求Patch进入批准流程。
type ApprovalPolicy struct{}

// NewApprovalPolicy 创建无进程级grant的默认策略。
func NewApprovalPolicy() ApprovalPolicy { return ApprovalPolicy{} }

// Decide 对七能力执行默认安全策略。
func (ApprovalPolicy) Decide(call ReadyCall) PolicyDecision {
	if call.Validate() != nil {
		return PolicyDeny
	}
	switch call.Capability() {
	case CapabilityRead, CapabilityGlob, CapabilityGrep, CapabilityReadArtifact:
		return PolicyAllow
	case CapabilityPatch, CapabilityWrite, CapabilityExec:
		return PolicyAsk
	default:
		return PolicyDeny
	}
}

// CapabilityMismatchError 返回不泄露输入正文的稳定错误。
func CapabilityMismatchError(capability CapabilityID) error {
	return fmt.Errorf("tool capability %q does not match typed payload", capability)
}

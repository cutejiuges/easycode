package tool

import (
	"context"
	"errors"
)

const CapabilityReadArtifact CapabilityID = "artifact.read"

// ReadArtifactInvocation 保存与原生调用配对的已完成范围输入。
type ReadArtifactInvocation struct {
	id    InvocationID
	call  ProviderCallID
	input ReadArtifactInput
}

// NewReadArtifactReadyCall 创建完整范围调用。
func NewReadArtifactReadyCall(id ProviderCallID, input ReadArtifactInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: id, capability: CapabilityReadArtifact, wireKind: WireStructured, artifactInput: input})
}
func (call ReadyCall) ReadArtifactInput() ReadArtifactInput { return call.artifactInput }

// NewReadArtifactInvocation 绑定 durable invocation 身份。
func NewReadArtifactInvocation(id InvocationID, call ReadyCall) (ReadArtifactInvocation, error) {
	v := ReadArtifactInvocation{id, call.providerCallID, call.artifactInput}
	if !id.Valid() || call.capability != CapabilityReadArtifact || call.Validate() != nil {
		return ReadArtifactInvocation{}, errors.New("invalid ReadArtifact invocation")
	}
	return v, nil
}
func (v ReadArtifactInvocation) InvocationID() InvocationID     { return v.id }
func (v ReadArtifactInvocation) ProviderCallID() ProviderCallID { return v.call }
func (v ReadArtifactInvocation) Input() ReadArtifactInput       { return v.input }
func (v ReadArtifactInvocation) Validate() error {
	if !v.id.Valid() || !v.call.Valid() || v.input.Validate() != nil {
		return errors.New("invalid ReadArtifact invocation")
	}
	return nil
}
func (v Invocation) ReadArtifact() (ReadArtifactInvocation, bool) {
	return v.artifact, v.capability == CapabilityReadArtifact
}

// ReadArtifactExecutor 执行授权范围读取，不产生工作区读取证据。
type ReadArtifactExecutor interface {
	Execute(context.Context, ReadArtifactInvocation) InvocationResult
}

// NewReadArtifactInvocationResult 冻结结果；错误结果不携带成功页。
func NewReadArtifactInvocationResult(v ReadArtifactInvocation, status ResultStatus, code string, preview ModelPreview, page ArtifactPage) (InvocationResult, error) {
	if v.Validate() != nil {
		return InvocationResult{}, errors.New("invalid ReadArtifact invocation")
	}
	r := InvocationResult{invocationID: v.id, providerCallID: v.call, capability: CapabilityReadArtifact, wireKind: WireStructured, status: status, code: code, preview: preview, artifactInput: v.input, artifactPage: page}
	return r, r.Validate()
}
func (r InvocationResult) ReadArtifactInput() ReadArtifactInput { return r.artifactInput }
func (r InvocationResult) ArtifactPage() (ArtifactPage, bool) {
	return r.artifactPage, r.capability == CapabilityReadArtifact && r.status == ResultSuccess
}

func (r InvocationResult) validateArtifactResult() error {
	if r.wireKind != WireStructured || r.artifactInput.Validate() != nil || r.readMetadata != (ReadResultMetadata{}) || !r.globMetadata.zero() || !r.grepMetadata.zero() || !r.patchMetadata.zero() {
		return errors.New("invalid ReadArtifact metadata")
	}
	if r.status == ResultSuccess {
		p := r.artifactPage
		if p.Validate() != nil || p.ID() != r.artifactInput.ID() || p.Offset() != r.artifactInput.Offset() || p.NextOffset()-p.Offset() > r.artifactInput.Limit() {
			return errors.New("ReadArtifact page mismatch")
		}
		preview, err := p.Preview()
		if err != nil || preview.Text() != r.preview.Text() {
			return errors.New("ReadArtifact preview mismatch")
		}
	} else if r.artifactPage != (ArtifactPage{}) || r.status == ResultConflict {
		return errors.New("invalid ReadArtifact error metadata")
	}
	return nil
}

package tool

import (
	"context"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"fmt"
	"unicode/utf8"
)

const CapabilitySkill CapabilityID = "skill.read"

// SkillInput 是主文档加载与辅助资源读取的封闭输入。
type SkillInput struct{ name, args, resource string }

func NewSkillLoadInput(name, args string) (SkillInput, error) {
	v := SkillInput{name: name, args: args}
	return v, v.Validate()
}
func NewSkillResourceInput(name, resource string) (SkillInput, error) {
	v := SkillInput{name: name, resource: resource}
	if resource == "" {
		return SkillInput{}, fmt.Errorf("skill resource is required")
	}
	return v, v.Validate()
}
func (v SkillInput) Name() string      { return v.name }
func (v SkillInput) Arguments() string { return v.args }
func (v SkillInput) Resource() string  { return v.resource }
func (v SkillInput) Validate() error {
	if domain.ValidateSkillName(v.name) != nil || !utf8.ValidString(v.args) || len(v.args) > domain.MaxSkillArgumentBytes {
		return fmt.Errorf("invalid Skill input")
	}
	if v.resource != "" && (v.args != "" || domain.ValidateSkillResource(v.resource) != nil) {
		return fmt.Errorf("invalid Skill resource")
	}
	return nil
}
func DecodeSkillInput(b []byte) (SkillInput, error) {
	if len(b) > 128<<10 {
		return SkillInput{}, fmt.Errorf("skill input exceeds limit")
	}
	var w struct {
		Name     string  `json:"skill"`
		Args     *string `json:"args"`
		Resource *string `json:"resource"`
	}
	if e := codec.DecodeObject(b, &w, "skill"); e != nil {
		return SkillInput{}, e
	}
	if w.Resource != nil {
		if w.Args != nil {
			return SkillInput{}, fmt.Errorf("skill args and resource are exclusive")
		}
		return NewSkillResourceInput(w.Name, *w.Resource)
	}
	args := ""
	if w.Args != nil {
		args = *w.Args
	}
	return NewSkillLoadInput(w.Name, args)
}
func NewSkillReadyCall(id ProviderCallID, input SkillInput) (ReadyCall, error) {
	return newReadyCall(ReadyCall{providerCallID: id, capability: CapabilitySkill, wireKind: WireStructured, skillInput: input})
}
func (c ReadyCall) SkillInput() SkillInput { return c.skillInput }

// SkillInvocation 绑定唯一调用身份，不包含权限授予。
type SkillInvocation struct {
	id    InvocationID
	call  ProviderCallID
	input SkillInput
}

func NewSkillInvocation(id InvocationID, c ReadyCall) (SkillInvocation, error) {
	v := SkillInvocation{id, c.providerCallID, c.skillInput}
	if c.capability != CapabilitySkill || c.Validate() != nil {
		return SkillInvocation{}, fmt.Errorf("invalid Skill call")
	}
	return v, v.Validate()
}
func (v SkillInvocation) Validate() error {
	if !v.id.Valid() || !v.call.Valid() || v.input.Validate() != nil {
		return fmt.Errorf("invalid Skill invocation")
	}
	return nil
}
func (v SkillInvocation) InvocationID() InvocationID     { return v.id }
func (v SkillInvocation) ProviderCallID() ProviderCallID { return v.call }
func (v SkillInvocation) Input() SkillInput              { return v.input }
func (v Invocation) Skill() (SkillInvocation, bool)      { return v.skill, v.capability == CapabilitySkill }

// SkillExecutor 接收已提交的当前 turn 显式选择；不得跨 turn 缓存授权。
type SkillExecutor interface {
	Execute(context.Context, SkillInvocation, []domain.SkillAttachment) InvocationResult
}

func NewSkillInvocationResult(v SkillInvocation, status ResultStatus, code string, p ModelPreview, a domain.SkillAttachment) (InvocationResult, error) {
	if v.Validate() != nil {
		return InvocationResult{}, fmt.Errorf("invalid Skill invocation")
	}
	r := InvocationResult{invocationID: v.id, providerCallID: v.call, capability: CapabilitySkill, wireKind: WireStructured, status: status, code: code, preview: p, skillInput: v.input, skillAttachment: a}
	return initializeResultBudget(r)
}
func (r InvocationResult) SkillInput() SkillInput { return r.skillInput }
func (r InvocationResult) SkillAttachment() (domain.SkillAttachment, bool) {
	return r.skillAttachment, r.capability == CapabilitySkill && r.status == ResultSuccess
}
func (r InvocationResult) validateSkillResult() error {
	if r.budget.allowance > domain.MaxSkillContentBytes || r.wireKind != WireStructured || r.skillInput.Validate() != nil || r.readMetadata != (ReadResultMetadata{}) || !r.globMetadata.zero() || !r.grepMetadata.zero() || !r.patchMetadata.zero() || len(r.preview.text) > domain.MaxSkillContentBytes {
		return fmt.Errorf("invalid Skill result")
	}
	if r.status == ResultSuccess {
		if r.skillAttachment.Validate() != nil || r.skillAttachment.Name != r.skillInput.name || r.skillAttachment.Arguments != r.skillInput.args || r.skillAttachment.Content != r.preview.text {
			return fmt.Errorf("skill result content mismatch")
		}
	} else if r.skillAttachment != (domain.SkillAttachment{}) {
		return fmt.Errorf("unexpected Skill attachment")
	}
	return nil
}
func NewSkillMaterial(r InvocationResult) (ReadonlyMaterial, error) {
	if r.Capability() != CapabilitySkill || r.Validate() != nil {
		return ReadonlyMaterial{}, fmt.Errorf("invalid Skill material")
	}
	return ReadonlyMaterial{capability: CapabilitySkill, result: r}, nil
}

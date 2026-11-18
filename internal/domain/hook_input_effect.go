package domain

import (
	"encoding/json"

	"easycode/internal/codec"
)

// HookInputEffectKind 是唯一当前 Pre finished 的封闭输入变化。
type HookInputEffectKind string

const (
	HookInputUnchanged HookInputEffectKind = "unchanged"
	HookInputReplaced  HookInputEffectKind = "replaced"
)

// HookInputEffect 保存候选摘要链；完整输入仅出现在真实变化分支。
type HookInputEffect struct {
	kind   HookInputEffectKind
	before string
	after  string
	input  HookToolInput
}

func NewUnchangedHookInputEffect(input HookToolInput) (HookInputEffect, error) {
	digest, err := input.Digest()
	if err != nil {
		return HookInputEffect{}, err
	}
	return HookInputEffect{kind: HookInputUnchanged, before: digest, after: digest}, nil
}
func NewReplacedHookInputEffect(before, after HookToolInput, profile HookInputProfile) (HookInputEffect, error) {
	if before.Capability() != after.Capability() || before.ValidateProfile(profile) != nil || after.ValidateReplacement(profile) != nil || before.Equal(after) {
		return HookInputEffect{}, HookOutputInvalid
	}
	b, err := before.Digest()
	if err != nil {
		return HookInputEffect{}, err
	}
	a, err := after.Digest()
	if err != nil {
		return HookInputEffect{}, err
	}
	return HookInputEffect{kind: HookInputReplaced, before: b, after: a, input: after.Clone()}, nil
}
func (v HookInputEffect) Kind() HookInputEffectKind { return v.kind }
func (v HookInputEffect) BeforeDigest() string      { return v.before }
func (v HookInputEffect) AfterDigest() string       { return v.after }
func (v HookInputEffect) Input() HookToolInput      { return v.input.Clone() }
func (v HookInputEffect) Empty() bool {
	return v.kind == "" && v.before == "" && v.after == "" && v.input.capability == ""
}
func (v HookInputEffect) Clone() HookInputEffect { v.input = v.input.Clone(); return v }
func (v HookInputEffect) Validate(capability HookCapability) error {
	if !capability.Valid() || !ValidHookDigest(v.before) || !ValidHookDigest(v.after) {
		return HookConfigInvalid
	}
	switch v.kind {
	case HookInputUnchanged:
		if v.before != v.after || v.input.capability != "" {
			return HookConfigInvalid
		}
	case HookInputReplaced:
		b, digest, err := v.input.encodingAndDigest()
		if err != nil || len(b) > MaxHookRewriteBytes || v.input.capability != capability || v.before == v.after || digest != v.after {
			return HookConfigInvalid
		}
	default:
		return HookConfigInvalid
	}
	return nil
}

// ApplyAgainst 只推导检查候选，不表示 gate 已选择该值或授予执行权限。
func (v HookInputEffect) ApplyAgainst(context HookRewriteContext) (HookToolInput, error) {
	digest, err := context.input.Digest()
	if context.Validate() != nil || err != nil || v.Validate(context.input.capability) != nil || v.before != digest {
		return HookToolInput{}, HookConfigInvalid
	}
	if v.kind == HookInputUnchanged {
		return context.Input(), nil
	}
	if v.input.ValidateReplacement(context.profile) != nil {
		return HookToolInput{}, HookConfigInvalid
	}
	return v.input.Clone(), nil
}
func (v HookInputEffect) MarshalJSON() ([]byte, error) {
	if v.kind == HookInputUnchanged {
		return codec.MarshalStable(struct {
			Kind   HookInputEffectKind `json:"kind"`
			Before string              `json:"before_digest"`
			After  string              `json:"after_digest"`
		}{v.kind, v.before, v.after})
	}
	if v.kind != HookInputReplaced {
		return nil, HookConfigInvalid
	}
	input, err := v.input.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return codec.MarshalStable(struct {
		Kind   HookInputEffectKind `json:"kind"`
		Before string              `json:"before_digest"`
		After  string              `json:"after_digest"`
		Input  json.RawMessage     `json:"input"`
	}{v.kind, v.before, v.after, input})
}
func DecodeHookInputEffect(capability HookCapability, b []byte) (HookInputEffect, error) {
	if len(b) > MaxHookConfigBytes {
		return HookInputEffect{}, HookConfigInvalid
	}
	members, err := codec.ObjectMembers(b)
	if err != nil {
		return HookInputEffect{}, HookConfigInvalid
	}
	var kind HookInputEffectKind
	if codec.UnmarshalRecord(members["kind"], &kind) != nil {
		return HookInputEffect{}, HookConfigInvalid
	}
	var wire struct {
		Kind   HookInputEffectKind `json:"kind"`
		Before string              `json:"before_digest"`
		After  string              `json:"after_digest"`
		Input  json.RawMessage     `json:"input,omitempty"`
	}
	if codec.UnmarshalRecord(b, &wire) != nil {
		return HookInputEffect{}, HookConfigInvalid
	}
	v := HookInputEffect{kind: kind, before: wire.Before, after: wire.After}
	_, hasInput := members["input"]
	if hasInput != (kind == HookInputReplaced) {
		return HookInputEffect{}, HookConfigInvalid
	}
	if hasInput {
		v.input, err = DecodeHookToolInput(capability, wire.Input)
		if err != nil {
			return HookInputEffect{}, HookConfigInvalid
		}
		// 持久化对象同样要求完整成员，不能用一般输入 decoder 补齐零值。
		var profile HookInputProfile
		switch capability {
		case HookPatch:
			_, rs, _ := v.input.Patch()
			profile = HookSubstringPatch
			if len(rs) > 0 && rs[0].LineBased {
				profile = HookLinePatch
			}
		case HookWrite:
			profile = HookWriteProfile
		case HookExec:
			profile = HookExecProfile
		}
		context, contextErr := NewHookRewriteContext(v.input, profile)
		if contextErr != nil {
			return HookInputEffect{}, HookConfigInvalid
		}
		parsed, parseErr := DecodeHookReplacement(context, wire.Input)
		if parseErr != nil {
			return HookInputEffect{}, HookConfigInvalid
		}
		v.input = parsed.Input()
	}
	return v, v.Validate(capability)
}

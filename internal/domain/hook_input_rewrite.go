package domain

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"

	"easycode/internal/codec"
)

// MaxHookRewriteBytes 独立限制规范参数，原始流大小不能约束 JSON 转义膨胀。
const MaxHookRewriteBytes = 16 << 10

// HookInputProfile 描述来源允许的匹配语义，不包含 Provider wire。
type HookInputProfile string

const (
	HookSubstringPatch HookInputProfile = "substring_patch"
	HookLinePatch      HookInputProfile = "line_patch"
	HookWriteProfile   HookInputProfile = "write"
	HookExecProfile    HookInputProfile = "exec"
)

func (p HookInputProfile) ValidFor(capability HookCapability) bool {
	switch p {
	case HookSubstringPatch, HookLinePatch:
		return capability == HookPatch
	case HookWriteProfile:
		return capability == HookWrite
	case HookExecProfile:
		return capability == HookExec
	default:
		return false
	}
}

func (v HookToolInput) Patch() (string, []HookReplacement, bool) {
	return v.patch.FilePath, append([]HookReplacement(nil), v.patch.Replacements...), v.capability == HookPatch
}
func (v HookToolInput) Write() (string, string, bool) {
	return v.write.FilePath, v.write.Content, v.capability == HookWrite
}
func (v HookToolInput) Exec() (string, string, int, bool) {
	return v.exec.Command, v.exec.Workdir, v.exec.TimeoutMillis, v.capability == HookExec
}

// Digest 只包含规范能力与参数，不包含授权、来源路径或运行身份。
func (v HookToolInput) Digest() (string, error) {
	_, digest, err := v.encodingAndDigest()
	return digest, err
}

func (v HookToolInput) encodingAndDigest() ([]byte, string, error) {
	input, err := v.validatedJSON()
	if err != nil {
		return nil, "", err
	}
	// 能力已由上面的校验限定为三个固定名称，输入字节也已由 Sonic 生成；只补齐固定摘要 envelope。
	prefix := `{"capability":"` + string(v.capability) + `","input":`
	b := make([]byte, 0, len(prefix)+len(input)+1)
	b = append(b, prefix...)
	b = append(b, input...)
	b = append(b, '}')
	return input, HookDigest(b), nil
}
func (v HookToolInput) Equal(other HookToolInput) bool {
	left, le := v.MarshalJSON()
	right, re := other.MarshalJSON()
	return v.capability == other.capability && le == nil && re == nil && bytes.Equal(left, right)
}

// ValidateProfile 约束来源语义；原始输入可以大于可替换对象的独立额度。
func (v HookToolInput) ValidateProfile(profile HookInputProfile) error {
	if v.Validate() != nil || !profile.ValidFor(v.capability) {
		return HookOutputInvalid
	}
	switch v.capability {
	case HookPatch:
		if profile == HookSubstringPatch && len(v.patch.Replacements) != 1 {
			return HookOutputInvalid
		}
		seen := make(map[HookReplacement]struct{}, len(v.patch.Replacements))
		for _, r := range v.patch.Replacements {
			if r.OldText == "" || r.OldText == r.NewText || strings.ContainsRune(r.OldText, 0) || strings.ContainsRune(r.NewText, 0) {
				return HookOutputInvalid
			}
			if _, duplicate := seen[r]; duplicate {
				return HookOutputInvalid
			}
			seen[r] = struct{}{}
			if profile == HookSubstringPatch && r.LineBased || profile == HookLinePatch && (!r.LineBased || r.ReplaceAll || strings.ContainsAny(r.OldText+r.NewText, "\r") || !strings.HasSuffix(r.OldText, "\n") || r.NewText != "" && !strings.HasSuffix(r.NewText, "\n")) {
				return HookOutputInvalid
			}
		}
	case HookWrite:
		if strings.ContainsRune(v.write.Content, 0) || len(strings.Split(v.write.FilePath, "/")) > 64 {
			return HookOutputInvalid
		}
		for _, r := range v.write.FilePath {
			if unicode.IsControl(r) {
				return HookOutputInvalid
			}
		}
	case HookExec:
		if v.exec.TimeoutMillis < 1000 || v.exec.TimeoutMillis > 600000 || len(v.exec.Command) > 64<<10 || v.exec.Workdir == "." {
			return HookOutputInvalid
		}
	}
	return nil
}

func (v HookToolInput) ValidateReplacement(profile HookInputProfile) error {
	if v.ValidateProfile(profile) != nil {
		return HookOutputInvalid
	}
	b, err := v.MarshalJSON()
	if err != nil || len(b) > MaxHookRewriteBytes {
		return HookOutputInvalid
	}
	return nil
}

// HookRewriteContext 由真实原调用建立，脚本不能自报能力或来源匹配模式。
type HookRewriteContext struct {
	input   HookToolInput
	profile HookInputProfile
}

func NewHookRewriteContext(input HookToolInput, profile HookInputProfile) (HookRewriteContext, error) {
	v := HookRewriteContext{input.Clone(), profile}
	return v, v.Validate()
}
func (v HookRewriteContext) Validate() error           { return v.input.ValidateProfile(v.profile) }
func (v HookRewriteContext) Empty() bool               { return v.profile == "" && v.input.capability == "" }
func (v HookRewriteContext) Input() HookToolInput      { return v.input.Clone() }
func (v HookRewriteContext) Profile() HookInputProfile { return v.profile }

// HookInputReplacement 只保存已校验的规范候选；空值明确表示没有替换输出。
type HookInputReplacement struct {
	context HookRewriteContext
}

func NewHookInputReplacement(input HookToolInput, profile HookInputProfile) (HookInputReplacement, error) {
	if input.ValidateReplacement(profile) != nil {
		return HookInputReplacement{}, HookOutputInvalid
	}
	c, err := NewHookRewriteContext(input, profile)
	return HookInputReplacement{context: c}, err
}
func (v HookInputReplacement) Empty() bool          { return v.context.Empty() }
func (v HookInputReplacement) Input() HookToolInput { return v.context.Input() }
func (v HookInputReplacement) Validate() error {
	if v.Empty() {
		return nil
	}
	return v.context.input.ValidateReplacement(v.context.profile)
}
func (v HookInputReplacement) ValidateAgainst(context HookRewriteContext) error {
	if context.Validate() != nil || v.Validate() != nil || !v.Empty() && v.context.profile != context.profile {
		return HookOutputInvalid
	}
	return nil
}

// DecodeHookReplacement 拒绝缺失成员，包括允许为空或 false 的必需成员。
func DecodeHookReplacement(context HookRewriteContext, b []byte) (HookInputReplacement, error) {
	if context.Validate() != nil || len(b) > MaxHookOutputBytes {
		return HookInputReplacement{}, HookOutputInvalid
	}
	var input HookToolInput
	var err error
	switch context.input.capability {
	case HookPatch:
		var wire struct {
			FilePath     string            `json:"file_path"`
			Replacements []json.RawMessage `json:"replacements"`
		}
		if codec.DecodeObject(b, &wire, "file_path", "replacements") != nil || len(wire.Replacements) == 0 || len(wire.Replacements) > 1024 {
			return HookInputReplacement{}, HookOutputInvalid
		}
		rs := make([]HookReplacement, len(wire.Replacements))
		for i, raw := range wire.Replacements {
			if codec.DecodeObject(raw, &rs[i], "old_text", "new_text", "replace_all", "line_based") != nil {
				return HookInputReplacement{}, HookOutputInvalid
			}
		}
		input, err = NewHookPatchInput(wire.FilePath, rs)
	case HookWrite:
		var wire hookWriteInput
		if codec.DecodeObject(b, &wire, "file_path", "content") != nil {
			return HookInputReplacement{}, HookOutputInvalid
		}
		input, err = NewHookWriteInput(wire.FilePath, wire.Content)
	case HookExec:
		var wire hookExecInput
		if codec.DecodeObject(b, &wire, "command", "workdir", "timeout_ms") != nil {
			return HookInputReplacement{}, HookOutputInvalid
		}
		input, err = NewHookExecInput(wire.Command, wire.Workdir, wire.TimeoutMillis)
	default:
		return HookInputReplacement{}, HookOutputInvalid
	}
	if err != nil {
		return HookInputReplacement{}, HookOutputInvalid
	}
	return NewHookInputReplacement(input, context.profile)
}

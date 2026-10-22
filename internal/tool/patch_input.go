package tool

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	// MaxPatchInputBytes 限制单次 Patch 的规范化输入总量。
	MaxPatchInputBytes = 1 << 20
	// MaxPatchReplacements 限制单文件 Patch 中的 replacement 数量。
	MaxPatchReplacements = 4096
)

// ProviderWireKind 描述 Provider 原生工具调用的封闭 wire 形态。
type ProviderWireKind string

const (
	// WireStructured 表示具有严格 JSON schema 的工具调用。
	WireStructured ProviderWireKind = "structured"
	// WireCustomFreeform 表示由工具自定义语法解析的自由文本调用。
	WireCustomFreeform ProviderWireKind = "custom_freeform"
)

// Valid 判断 wire kind 是否受当前实现支持。
func (kind ProviderWireKind) Valid() bool {
	switch kind {
	case WireStructured, WireCustomFreeform:
		return true
	default:
		return false
	}
}

// PatchReplacement 是对既有文本执行的不可变精确替换。
type PatchReplacement struct {
	oldText    string
	newText    string
	replaceAll bool
	lineBased  bool
}

// NewPatchReplacement 构造一个非空且会产生变化的精确替换。
func NewPatchReplacement(oldText string, newText string, replaceAll bool) (PatchReplacement, error) {
	replacement := PatchReplacement{oldText: oldText, newText: newText, replaceAll: replaceAll}
	if err := replacement.Validate(); err != nil {
		return PatchReplacement{}, err
	}
	return replacement, nil
}

// OldText 返回必须精确匹配的旧文本。
func (replacement PatchReplacement) OldText() string { return replacement.oldText }

// NewText 返回替换后的新文本。
func (replacement PatchReplacement) NewText() string { return replacement.newText }

// ReplaceAll 返回是否替换base中的全部精确匹配。
func (replacement PatchReplacement) ReplaceAll() bool { return replacement.replaceAll }

// LineBased 表示replacement匹配完整LF文本行而非任意子串。
func (replacement PatchReplacement) LineBased() bool { return replacement.lineBased }

// NewPatchLineReplacement 将完整hunk行规范化为带终止符的精确行替换。
func NewPatchLineReplacement(oldLines, newLines []string) (PatchReplacement, error) {
	if len(oldLines) == 0 {
		return PatchReplacement{}, errors.New("patch hunk requires existing lines")
	}
	oldText := strings.Join(oldLines, "\n") + "\n"
	newText := ""
	if len(newLines) > 0 {
		newText = strings.Join(newLines, "\n") + "\n"
	}
	replacement, err := NewPatchReplacement(oldText, newText, false)
	replacement.lineBased = true
	return replacement, err
}

// Validate 验证replacement编码、边界和非空变化。
func (replacement PatchReplacement) Validate() error {
	if replacement.lineBased && (replacement.replaceAll || !strings.HasSuffix(replacement.oldText, "\n") || (replacement.newText != "" && !strings.HasSuffix(replacement.newText, "\n"))) {
		return errors.New("patch line replacement is invalid")
	}
	if replacement.oldText == "" || !utf8.ValidString(replacement.oldText) || strings.ContainsRune(replacement.oldText, 0) {
		return errors.New("patch old text must be non-empty UTF-8")
	}
	if !utf8.ValidString(replacement.newText) || strings.ContainsRune(replacement.newText, 0) {
		return errors.New("patch new text must be UTF-8")
	}
	if replacement.oldText == replacement.newText {
		return errors.New("patch replacement must change content")
	}
	return nil
}

// PatchInput 是只描述一个既有workspace文件的不可变replacement计划。
type PatchInput struct {
	filePath     string
	replacements []PatchReplacement
}

// NewPatchInput 构造并深拷贝一个单文件Patch计划。
func NewPatchInput(filePath string, replacements []PatchReplacement) (PatchInput, error) {
	input := PatchInput{filePath: filePath, replacements: clonePatchReplacements(replacements)}
	if err := input.Validate(); err != nil {
		return PatchInput{}, err
	}
	return input, nil
}

// FilePath 返回规范化的workspace相对路径。
func (input PatchInput) FilePath() string { return input.filePath }

// Replacements 返回独立副本。
func (input PatchInput) Replacements() []PatchReplacement {
	return clonePatchReplacements(input.replacements)
}

// Clone 返回不共享可变slice的副本。
func (input PatchInput) Clone() PatchInput {
	return PatchInput{filePath: input.filePath, replacements: input.Replacements()}
}

// Equal 判断两个Patch输入是否具有完全相同的规范化语义。
func (input PatchInput) Equal(other PatchInput) bool {
	if input.filePath != other.filePath || len(input.replacements) != len(other.replacements) {
		return false
	}
	for index := range input.replacements {
		if input.replacements[index] != other.replacements[index] {
			return false
		}
	}
	return true
}

// Validate 验证路径、replacement联合、重复项和总输入预算。
func (input PatchInput) Validate() error {
	if input.filePath == "" {
		return errors.New("patch file path is required")
	}
	if err := validateSearchPath(input.filePath); err != nil {
		return errors.New("patch file path must be normalized workspace-relative UTF-8")
	}
	if len(input.replacements) == 0 || len(input.replacements) > MaxPatchReplacements {
		return errors.New("patch requires between 1 and 4096 replacements")
	}
	totalBytes := len(input.filePath)
	seen := make(map[PatchReplacement]struct{}, len(input.replacements))
	for _, replacement := range input.replacements {
		if err := replacement.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[replacement]; duplicate {
			return errors.New("patch contains duplicate replacements")
		}
		seen[replacement] = struct{}{}
		totalBytes += len(replacement.oldText) + len(replacement.newText)
		if totalBytes > MaxPatchInputBytes {
			return errors.New("patch input exceeds 1 MiB")
		}
	}
	return nil
}

func (input PatchInput) zero() bool {
	return input.filePath == "" && len(input.replacements) == 0
}

func clonePatchReplacements(source []PatchReplacement) []PatchReplacement {
	if source == nil {
		return nil
	}
	result := make([]PatchReplacement, len(source))
	copy(result, source)
	return result
}

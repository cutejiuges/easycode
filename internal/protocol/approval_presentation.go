package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/codec"
)

// ApprovalPresentationKind 区分当前三种文件批准展示，不能由内容是否为空推断。
type ApprovalPresentationKind string

const (
	PresentationPatch          ApprovalPresentationKind = "patch"
	PresentationWriteCreate    ApprovalPresentationKind = "write-create"
	PresentationWriteOverwrite ApprovalPresentationKind = "write-overwrite"
)

// PatchApprovalPresentation 保存既有完整 Patch diff。
type PatchApprovalPresentation struct {
	Diff string `json:"diff"`
}

// WriteCreateApprovalPresentation 保存完整新内容预览与目录计划。
type WriteCreateApprovalPresentation struct {
	Preview     string   `json:"preview"`
	Bytes       int      `json:"bytes"`
	Lines       int      `json:"lines"`
	Directories []string `json:"directories"`
}

// WriteOverwriteApprovalPresentation 保存覆盖的完整 diff 与最终计数。
type WriteOverwriteApprovalPresentation struct {
	Diff  string `json:"diff"`
	Bytes int    `json:"bytes"`
	Lines int    `json:"lines"`
}

// ApprovalPresentation 是具备专属构造与解码的当前封闭展示联合。
type ApprovalPresentation struct {
	Kind           ApprovalPresentationKind            `json:"kind"`
	Patch          *PatchApprovalPresentation          `json:"patch,omitempty"`
	WriteCreate    *WriteCreateApprovalPresentation    `json:"write_create,omitempty"`
	WriteOverwrite *WriteOverwriteApprovalPresentation `json:"write_overwrite,omitempty"`
}

// NewPatchApprovalPresentation 构造与现有 Patch 交互一致的展示。
func NewPatchApprovalPresentation(diff string) (ApprovalPresentation, error) {
	p := ApprovalPresentation{Kind: PresentationPatch, Patch: &PatchApprovalPresentation{Diff: diff}}
	return p, p.Validate()
}

// NewWriteCreateApprovalPresentation 构造创建文件展示并复制目录清单。
func NewWriteCreateApprovalPresentation(preview string, byteCount, lineCount int, directories []string) (ApprovalPresentation, error) {
	p := ApprovalPresentation{Kind: PresentationWriteCreate, WriteCreate: &WriteCreateApprovalPresentation{Preview: preview, Bytes: byteCount, Lines: lineCount, Directories: append([]string{}, directories...)}}
	return p, p.Validate()
}

// NewWriteOverwriteApprovalPresentation 构造覆盖展示，即使旧文件为空也保留覆盖语义。
func NewWriteOverwriteApprovalPresentation(diff string, byteCount, lineCount int) (ApprovalPresentation, error) {
	p := ApprovalPresentation{Kind: PresentationWriteOverwrite, WriteOverwrite: &WriteOverwriteApprovalPresentation{Diff: diff, Bytes: byteCount, Lines: lineCount}}
	return p, p.Validate()
}

// Validate 验证唯一分支、完整展示和所有必需的计数。
func (p ApprovalPresentation) Validate() error {
	var text string
	byteCount, lineCount := 0, 0
	directories := []string{}
	switch p.Kind {
	case PresentationPatch:
		if p.Patch == nil || p.WriteCreate != nil || p.WriteOverwrite != nil {
			return fmt.Errorf("invalid Patch presentation union")
		}
		text = p.Patch.Diff
	case PresentationWriteCreate:
		if p.Patch != nil || p.WriteCreate == nil || p.WriteOverwrite != nil || p.WriteCreate.Directories == nil {
			return fmt.Errorf("invalid Write create presentation union")
		}
		text, byteCount, lineCount, directories = p.WriteCreate.Preview, p.WriteCreate.Bytes, p.WriteCreate.Lines, p.WriteCreate.Directories
	case PresentationWriteOverwrite:
		if p.Patch != nil || p.WriteCreate != nil || p.WriteOverwrite == nil {
			return fmt.Errorf("invalid Write overwrite presentation union")
		}
		text, byteCount, lineCount = p.WriteOverwrite.Diff, p.WriteOverwrite.Bytes, p.WriteOverwrite.Lines
	default:
		return fmt.Errorf("unsupported approval presentation")
	}
	budget := len(text)
	if len(directories) > 16 {
		return fmt.Errorf("approval directory plan exceeds limit")
	}
	for i, path := range directories {
		if !validApprovalPath(path) || len(path) > 4096 || (i > 0 && !strings.HasPrefix(path, directories[i-1]+"/")) {
			return fmt.Errorf("invalid approval directory plan")
		}
		budget += len(VisibleApprovalText(path)) + len("Create directory: \n")
	}
	if p.Kind != PresentationPatch {
		budget += len(fmt.Sprintf("%d bytes, %d lines\n", byteCount, lineCount))
	}
	if text == "" || !utf8.ValidString(text) || budget > maxApprovalDiffBytes || byteCount < 0 || byteCount > 1<<20 || lineCount < 0 || lineCount > byteCount || (byteCount == 0) != (lineCount == 0) {
		return fmt.Errorf("invalid approval presentation content")
	}
	if p.Kind != PresentationPatch {
		for _, r := range text {
			if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) {
				return fmt.Errorf("write presentation contains terminal controls")
			}
		}
	}
	return nil
}

// Capability 返回展示绑定的唯一能力，未知分支保持无效。
func (p ApprovalPresentation) Capability() string {
	switch p.Kind {
	case PresentationPatch:
		return "fs.patch"
	case PresentationWriteCreate, PresentationWriteOverwrite:
		return "fs.write"
	default:
		return ""
	}
}

// Text 仅组合已冻结展示，窗口变化与渲染不触发 I/O。
func (p ApprovalPresentation) Text() string {
	if p.Validate() != nil {
		return ""
	}
	switch p.Kind {
	case PresentationPatch:
		return p.Patch.Diff
	case PresentationWriteOverwrite:
		return fmt.Sprintf("%d bytes, %d lines\n", p.WriteOverwrite.Bytes, p.WriteOverwrite.Lines) + p.WriteOverwrite.Diff
	case PresentationWriteCreate:
		var out strings.Builder
		fmt.Fprintf(&out, "%d bytes, %d lines\n", p.WriteCreate.Bytes, p.WriteCreate.Lines)
		for _, path := range p.WriteCreate.Directories {
			fmt.Fprintf(&out, "Create directory: %s\n", VisibleApprovalText(path))
		}
		out.WriteString(p.WriteCreate.Preview)
		return out.String()
	}
	return ""
}

// VisibleApprovalText 将路径中的不可见控制和反斜杠转换为明确文本。
func VisibleApprovalText(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r == '\\' {
			out.WriteString(`\\`)
		} else if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			fmt.Fprintf(&out, `\u{%04X}`, r)
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// UnmarshalJSON 拒绝缺失、null、混合分支和旧的扁平 diff shape。
func (p *ApprovalPresentation) UnmarshalJSON(data []byte) error {
	if err := codec.ValidateObjectFields(data, "kind"); err != nil {
		return err
	}
	var wire struct {
		Kind      ApprovalPresentationKind `json:"kind"`
		Patch     json.RawMessage          `json:"patch"`
		Create    json.RawMessage          `json:"write_create"`
		Overwrite json.RawMessage          `json:"write_overwrite"`
	}
	if codec.UnmarshalStrict(data, &wire) != nil {
		return fmt.Errorf("invalid approval presentation encoding")
	}
	var decoded ApprovalPresentation
	var err error
	switch wire.Kind {
	case PresentationPatch:
		if len(wire.Create) != 0 || len(wire.Overwrite) != 0 {
			return fmt.Errorf("mixed approval presentation")
		}
		if err := codec.ValidateObjectFields(wire.Patch, "diff"); err != nil {
			return err
		}
		var branch PatchApprovalPresentation
		if codec.UnmarshalStrict(wire.Patch, &branch) != nil {
			return fmt.Errorf("invalid Patch presentation")
		}
		decoded, err = NewPatchApprovalPresentation(branch.Diff)
	case PresentationWriteCreate:
		if len(wire.Patch) != 0 || len(wire.Overwrite) != 0 {
			return fmt.Errorf("mixed approval presentation")
		}
		if err := codec.ValidateObjectFields(wire.Create, "preview", "bytes", "lines", "directories"); err != nil {
			return err
		}
		var branch struct {
			Preview     *string   `json:"preview"`
			Bytes       *int      `json:"bytes"`
			Lines       *int      `json:"lines"`
			Directories *[]string `json:"directories"`
		}
		if codec.UnmarshalStrict(wire.Create, &branch) != nil || branch.Preview == nil || branch.Bytes == nil || branch.Lines == nil || branch.Directories == nil {
			return fmt.Errorf("incomplete Write create presentation")
		}
		decoded, err = NewWriteCreateApprovalPresentation(*branch.Preview, *branch.Bytes, *branch.Lines, *branch.Directories)
	case PresentationWriteOverwrite:
		if len(wire.Patch) != 0 || len(wire.Create) != 0 {
			return fmt.Errorf("mixed approval presentation")
		}
		if err := codec.ValidateObjectFields(wire.Overwrite, "diff", "bytes", "lines"); err != nil {
			return err
		}
		var branch struct {
			Diff  *string `json:"diff"`
			Bytes *int    `json:"bytes"`
			Lines *int    `json:"lines"`
		}
		if codec.UnmarshalStrict(wire.Overwrite, &branch) != nil || branch.Diff == nil || branch.Bytes == nil || branch.Lines == nil {
			return fmt.Errorf("incomplete Write overwrite presentation")
		}
		decoded, err = NewWriteOverwriteApprovalPresentation(*branch.Diff, *branch.Bytes, *branch.Lines)
	default:
		return fmt.Errorf("unsupported approval presentation")
	}
	if err != nil {
		return err
	}
	*p = decoded
	return nil
}

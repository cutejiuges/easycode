package domain

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	MaxFileReferences       = 8
	MaxFilePathBytes        = 4096
	MaxFileEditBytes        = 24578
	MaxFileSourceBytes      = 16 << 20
	MaxFileInputSourceBytes = 64 << 20
	MaxFileRenderedBytes    = 64 << 10
	MaxFileAttachmentsBytes = 512 << 10
)

// FileFailureKind 是不含来源正文和底层错误的封闭失败类别。
type FileFailureKind string

const (
	FileInvalid             FileFailureKind = "file_reference_invalid"
	FileOutsideWorkspace    FileFailureKind = "file_reference_outside_workspace"
	FileUnavailable         FileFailureKind = "file_reference_unavailable"
	FileUnsupported         FileFailureKind = "file_reference_unsupported"
	FileEncoding            FileFailureKind = "file_reference_encoding"
	FileChanged             FileFailureKind = "file_reference_changed"
	FileLimit               FileFailureKind = "file_reference_limit"
	FileBusy                FileFailureKind = "file_reference_busy"
	FilePlatformUnsupported FileFailureKind = "unsupported_platform"
)

// FileFailure 允许边界按类型映射错误，不保留私密来源。
type FileFailure struct{ kind FileFailureKind }

// NewFileFailure 只接受当前实际使用的失败类别。
func NewFileFailure(kind FileFailureKind) *FileFailure {
	switch kind {
	case FileInvalid, FileOutsideWorkspace, FileUnavailable, FileUnsupported, FileEncoding, FileChanged, FileLimit, FileBusy, FilePlatformUnsupported:
		return &FileFailure{kind: kind}
	default:
		return &FileFailure{kind: FileInvalid}
	}
}

func (e *FileFailure) Error() string         { return string(e.kind) }
func (e *FileFailure) Kind() FileFailureKind { return e.kind }

// FileSelection 是 whole 或正整数闭区间的不可变联合。
type FileSelection struct {
	kind       string
	start, end uint64
}

// NewWholeFileSelection 明确选择文件整体，实际正文仍受预算限制。
func NewWholeFileSelection() FileSelection { return FileSelection{kind: "whole"} }

// NewLineFileSelection 校验闭区间，不将溢出或零解释成缺省。
func NewLineFileSelection(start, end uint64) (FileSelection, error) {
	v := FileSelection{kind: "lines", start: start, end: end}
	return v, v.Validate()
}

func (s FileSelection) Kind() string      { return s.kind }
func (s FileSelection) StartLine() uint64 { return s.start }
func (s FileSelection) EndLine() uint64   { return s.end }

// Validate 拒绝零值以及不属于分支的行字段。
func (s FileSelection) Validate() error {
	if s.kind == "whole" && s.start == 0 && s.end == 0 || s.kind == "lines" && s.start > 0 && s.end >= s.start {
		return nil
	}
	return NewFileFailure(FileInvalid)
}

func (s FileSelection) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.kind == "whole" {
		return codec.MarshalStable(struct {
			Kind string `json:"kind"`
		}{s.kind})
	}
	return codec.MarshalStable(struct {
		Kind  string `json:"kind"`
		Start uint64 `json:"start_line"`
		End   uint64 `json:"end_line"`
	}{s.kind, s.start, s.end})
}

// DecodeFileSelection 严格校验各分支唯一字段及必需字段。
func DecodeFileSelection(data []byte) (FileSelection, error) {
	var tag struct {
		Kind string `json:"kind"`
	}
	if len(data) > 256 || codec.Unmarshal(data, &tag) != nil {
		return FileSelection{}, NewFileFailure(FileInvalid)
	}
	switch tag.Kind {
	case "whole":
		if codec.DecodeObject(data, &tag, "kind") != nil {
			break
		}
		return NewWholeFileSelection(), nil
	case "lines":
		var value struct {
			Kind  string `json:"kind"`
			Start uint64 `json:"start_line"`
			End   uint64 `json:"end_line"`
		}
		if codec.DecodeObject(data, &value, "kind", "start_line", "end_line") != nil {
			break
		}
		return NewLineFileSelection(value.Start, value.End)
	}
	return FileSelection{}, NewFileFailure(FileInvalid)
}

func (s *FileSelection) UnmarshalJSON(data []byte) error {
	v, err := DecodeFileSelection(data)
	if err == nil {
		*s = v
	}
	return err
}

// FileReference 只保存规范工作区相对路径和选择，不保存根或推理设置。
type FileReference struct {
	path      string
	selection FileSelection
}

// NewFileReference 只在内存去除前导 ./，拒绝路径展开和非规范组件。
func NewFileReference(path string, selection FileSelection) (FileReference, error) {
	for strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	if path == "" || len(path) > MaxFilePathBytes || !utf8.ValidString(path) || selection.Validate() != nil {
		return FileReference{}, NewFileFailure(FileInvalid)
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") || strings.HasPrefix(path, "\\") || len(path) >= 2 && path[1] == ':' {
		return FileReference{}, NewFileFailure(FileOutsideWorkspace)
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return FileReference{}, NewFileFailure(FileOutsideWorkspace)
		}
		if part == "" || part == "." {
			return FileReference{}, NewFileFailure(FileInvalid)
		}
	}
	for _, r := range path {
		if unicode.IsControl(r) || r == '\\' {
			return FileReference{}, NewFileFailure(FileInvalid)
		}
	}
	return FileReference{path: strings.Clone(path), selection: selection}, nil
}

func (r FileReference) Path() string             { return r.path }
func (r FileReference) Selection() FileSelection { return r.selection }

func (r FileReference) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(struct {
		Path      string        `json:"path"`
		Selection FileSelection `json:"selection"`
	}{r.path, r.selection})
}

// DecodeFileReference 只接受唯一当前的规范路径与选择结构。
func DecodeFileReference(data []byte) (FileReference, error) {
	var value struct {
		Path      string        `json:"path"`
		Selection FileSelection `json:"selection"`
	}
	if len(data) > MaxFileEditBytes+256 || codec.DecodeObject(data, &value, "path", "selection") != nil {
		return FileReference{}, NewFileFailure(FileInvalid)
	}
	ref := FileReference{value.Path, value.Selection}
	return ref, ref.Validate()
}

func (r *FileReference) UnmarshalJSON(data []byte) error {
	value, err := DecodeFileReference(data)
	if err == nil {
		*r = value
	}
	return err
}

// Validate 同时要求路径已经规范化，不能在存储读取中静默重写。
func (r FileReference) Validate() error {
	v, err := NewFileReference(r.path, r.selection)
	if err != nil {
		return err
	}
	if v.path != r.path {
		return NewFileFailure(FileInvalid)
	}
	return nil
}

// RenderFilePath 生成与引用解析器往返一致的路径部分，不含 sigil 和行范围。
func RenderFilePath(path string) (string, error) {
	r, err := NewFileReference(path, NewWholeFileSelection())
	if err != nil || r.path != path {
		return "", NewFileFailure(FileInvalid)
	}
	quote := strings.HasPrefix(path, "@") || strings.ContainsAny(path, "#\"`~")
	for _, c := range path {
		quote = quote || unicode.IsSpace(c)
	}
	if !quote {
		return path, nil
	}
	data, err := codec.MarshalStable(path)
	return string(data), err
}

// String 用于显式插入，保留真实完整路径而非显示标签。
func (r FileReference) String() string {
	path, err := RenderFilePath(r.path)
	if err != nil {
		return ""
	}
	if r.selection.kind == "whole" {
		return "@" + path
	}
	if r.selection.start == r.selection.end {
		return fmt.Sprintf("@%s#L%d", path, r.selection.start)
	}
	return fmt.Sprintf("@%s#L%d-%d", path, r.selection.start, r.selection.end)
}

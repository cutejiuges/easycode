package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

const fileAttachmentHeading = "# Attached workspace file\n"

type fileAttachmentContent struct {
	Path            string        `json:"path"`
	Selection       FileSelection `json:"selection"`
	TotalLines      uint64        `json:"total_lines"`
	ActualStartLine uint64        `json:"actual_start_line"`
	ActualEndLine   uint64        `json:"actual_end_line"`
	EndsMidLine     bool          `json:"ends_mid_line"`
	Truncated       bool          `json:"truncated"`
	Content         string        `json:"content"`
}

type fileAttachmentRecord struct {
	Path            string        `json:"path"`
	Selection       FileSelection `json:"selection"`
	TotalLines      uint64        `json:"total_lines"`
	ActualStartLine uint64        `json:"actual_start_line"`
	ActualEndLine   uint64        `json:"actual_end_line"`
	EndsMidLine     bool          `json:"ends_mid_line"`
	Truncated       bool          `json:"truncated"`
	Content         string        `json:"content"`
	Digest          string        `json:"digest"`
}

// FileAttachment 私有持有冻结字段；字符串和值副本不共享可修改容器。
type FileAttachment struct {
	value  fileAttachmentContent
	digest string
}

func countFileLines(content string) uint64 {
	if content == "" {
		return 0
	}
	count := uint64(strings.Count(content, "\n"))
	if !strings.HasSuffix(content, "\n") {
		count++
	}
	return count
}

func fileLineSpan(content string, first, last uint64) (int, int) {
	start, end, line := 0, len(content), uint64(1)
	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}
		if line < first {
			start = i + 1
		}
		if line == last {
			end = i + 1
			break
		}
		line++
	}
	return start, end
}

// FreezeFileAttachment 从已安全读取的源内存选取原始字节，不访问来源。
func FreezeFileAttachment(ref FileReference, source string) (FileAttachment, error) {
	if err := ref.Validate(); err != nil {
		return FileAttachment{}, err
	}
	if len(source) > MaxFileSourceBytes {
		return FileAttachment{}, NewFileFailure(FileLimit)
	}
	if !utf8.ValidString(source) || strings.IndexByte(source, 0) >= 0 {
		return FileAttachment{}, NewFileFailure(FileEncoding)
	}
	total := countFileLines(source)
	first, requestedLast, lineLimit := uint64(1), total, uint64(200)
	if ref.selection.kind == "lines" {
		first, requestedLast, lineLimit = ref.selection.start, min(ref.selection.end, total), 2000
	}
	if ref.selection.kind == "lines" && first > total {
		return FileAttachment{}, NewFileFailure(FileUnavailable)
	}
	if total == 0 {
		return newFrozenFileAttachment(fileAttachmentContent{Path: ref.path, Selection: ref.selection})
	}
	last := min(requestedLast, first+min(lineLimit-1, total-first))
	start, end := fileLineSpan(source, first, last)
	_, requestedEnd := fileLineSpan(source, first, requestedLast)
	value := fileAttachmentContent{Path: ref.path, Selection: ref.selection, TotalLines: total, ActualStartLine: first, ActualEndLine: last}
	// 以最坏布尔长度和完整行号计算头部，再逐字符计算 JSON 膨胀；只最终编码一次。
	header, err := codec.MarshalStable(value)
	if err != nil {
		return FileAttachment{}, NewFileFailure(FileInvalid)
	}
	available := MaxFileRenderedBytes - len(fileAttachmentHeading) - len(header) - 1
	kept := 0
	for kept < end-start {
		r, size := utf8.DecodeRuneInString(source[start+kept : end])
		cost := size
		switch r {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
			cost = 2
		case '<', '>', '&', '\u2028', '\u2029':
			cost = 6
		default:
			if r < 0x20 {
				cost = 6
			}
		}
		if cost > available {
			break
		}
		available -= cost
		kept += size
	}
	if kept > 0 && start+kept < len(source) && source[start+kept-1] == '\r' && source[start+kept] == '\n' {
		kept--
	}
	if kept == 0 {
		return FileAttachment{}, NewFileFailure(FileLimit)
	}
	value.Content = strings.Clone(source[start : start+kept])
	value.ActualEndLine = first + countFileLines(value.Content) - 1
	value.EndsMidLine = start+kept < end && !strings.HasSuffix(value.Content, "\n")
	value.Truncated = start+kept < requestedEnd
	return newFrozenFileAttachment(value)
}

func newFrozenFileAttachment(value fileAttachmentContent) (FileAttachment, error) {
	data, err := codec.MarshalStable(value)
	if err != nil {
		return FileAttachment{}, NewFileFailure(FileInvalid)
	}
	digest := sha256.Sum256(data)
	a := FileAttachment{value: value, digest: hex.EncodeToString(digest[:])}
	return a, a.Validate()
}

func (a FileAttachment) Reference() FileReference {
	return FileReference{a.value.Path, a.value.Selection}
}
func (a FileAttachment) TotalLines() uint64      { return a.value.TotalLines }
func (a FileAttachment) ActualStartLine() uint64 { return a.value.ActualStartLine }
func (a FileAttachment) ActualEndLine() uint64   { return a.value.ActualEndLine }
func (a FileAttachment) EndsMidLine() bool       { return a.value.EndsMidLine }
func (a FileAttachment) Truncated() bool         { return a.value.Truncated }
func (a FileAttachment) Content() string         { return a.value.Content }
func (a FileAttachment) Digest() string          { return a.digest }

// RenderedText 是模型实际获得的唯一数据包装，不包含摘要和 digest。
func (a FileAttachment) RenderedText() (string, error) {
	if err := a.Validate(); err != nil {
		return "", err
	}
	data, err := codec.MarshalStable(a.value)
	if err != nil {
		return "", NewFileFailure(FileInvalid)
	}
	return fileAttachmentHeading + string(data) + "\n", nil
}

// Validate 交叉核对行跨度、完整性、选择、包装额度与内容摘要。
func (a FileAttachment) Validate() error {
	v := a.value
	if a.Reference().Validate() != nil || v.TotalLines > MaxFileSourceBytes || !utf8.ValidString(v.Content) || strings.IndexByte(v.Content, 0) >= 0 {
		return NewFileFailure(FileInvalid)
	}
	if v.TotalLines == 0 {
		if v.Selection.kind != "whole" || v.ActualStartLine != 0 || v.ActualEndLine != 0 || v.Content != "" || v.Truncated || v.EndsMidLine {
			return NewFileFailure(FileInvalid)
		}
	} else {
		first, last, maxLines := uint64(1), v.TotalLines, uint64(200)
		if v.Selection.kind == "lines" {
			first, last, maxLines = v.Selection.start, min(v.Selection.end, v.TotalLines), 2000
		}
		if first > v.TotalLines || v.Content == "" || v.ActualStartLine != first || v.ActualEndLine < first || v.ActualEndLine > last || v.ActualEndLine-first >= maxLines || countFileLines(v.Content) != v.ActualEndLine-first+1 {
			return NewFileFailure(FileInvalid)
		}
		newline := strings.HasSuffix(v.Content, "\n")
		if v.EndsMidLine && (newline || !v.Truncated) || !v.EndsMidLine && v.ActualEndLine < v.TotalLines && !newline || v.Truncated != (v.ActualEndLine < last || v.EndsMidLine) {
			return NewFileFailure(FileInvalid)
		}
	}
	data, err := codec.MarshalStable(v)
	if err != nil || len(fileAttachmentHeading)+len(data)+1 > MaxFileRenderedBytes {
		return NewFileFailure(FileLimit)
	}
	digest := sha256.Sum256(data)
	if a.digest != hex.EncodeToString(digest[:]) {
		return NewFileFailure(FileInvalid)
	}
	return nil
}

func (a FileAttachment) record() fileAttachmentRecord {
	v := a.value
	return fileAttachmentRecord{v.Path, v.Selection, v.TotalLines, v.ActualStartLine, v.ActualEndLine, v.EndsMidLine, v.Truncated, v.Content, a.digest}
}

func (a FileAttachment) MarshalJSON() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(a.record())
}

// DecodeFileAttachment 拒绝缺失、null、重复或额外成员，不重截断存储正文。
func DecodeFileAttachment(data []byte) (FileAttachment, error) {
	var v fileAttachmentRecord
	if len(data) > 2*MaxFileRenderedBytes || codec.DecodeObject(data, &v, "path", "selection", "total_lines", "actual_start_line", "actual_end_line", "ends_mid_line", "truncated", "content", "digest") != nil {
		return FileAttachment{}, NewFileFailure(FileInvalid)
	}
	a := FileAttachment{fileAttachmentContent{v.Path, v.Selection, v.TotalLines, v.ActualStartLine, v.ActualEndLine, v.EndsMidLine, v.Truncated, v.Content}, v.Digest}
	return a, a.Validate()
}

func (a *FileAttachment) UnmarshalJSON(data []byte) error {
	v, err := DecodeFileAttachment(data)
	if err == nil {
		*a = v
	}
	return err
}

// ByteSize 对当前元数据编码计费，不以原始正文长度代替实际字段开销。
func (a FileAttachment) ByteSize() int {
	data, err := codec.MarshalStable(a.record())
	if err != nil {
		return MaxUserInputBytes + 1
	}
	return len(data)
}

// FileAttachments 私有持有有序集合；零值明确表示无文件。
type FileAttachments struct{ items []FileAttachment }

// NewFileAttachments 完整验证集合后复制，不允许同一引用重复附带。
func NewFileAttachments(items []FileAttachment) (FileAttachments, error) {
	if len(items) > MaxFileReferences {
		return FileAttachments{}, NewFileFailure(FileLimit)
	}
	bytes := 0
	for i, item := range items {
		if err := item.Validate(); err != nil {
			return FileAttachments{}, err
		}
		for _, previous := range items[:i] {
			if previous.Reference() == item.Reference() {
				return FileAttachments{}, NewFileFailure(FileInvalid)
			}
		}
		text, err := item.RenderedText()
		if err != nil {
			return FileAttachments{}, err
		}
		bytes += len(text)
	}
	if bytes > MaxFileAttachmentsBytes {
		return FileAttachments{}, NewFileFailure(FileLimit)
	}
	return FileAttachments{append([]FileAttachment(nil), items...)}, nil
}

func (a FileAttachments) Items() []FileAttachment { return append([]FileAttachment(nil), a.items...) }

// Clone 复制集合而不执行来源校验或重新包装正文。
func (a FileAttachments) Clone() FileAttachments { return FileAttachments{items: a.Items()} }

// ValidateFileAttachments 核对完整有序引用集合；无附件的普通文字允许包含字面 @。
func ValidateFileAttachments(text string, files FileAttachments) error {
	if err := files.Validate(); err != nil {
		return err
	}
	if files.Len() == 0 {
		return nil
	}
	refs, err := ParseFileReferences(text)
	if err != nil {
		return err
	}
	actual := make([]FileReference, 0, files.Len())
	for _, item := range files.items {
		actual = append(actual, item.Reference())
	}
	if !slices.Equal(refs, actual) {
		return NewFileFailure(FileInvalid)
	}
	return nil
}
func (a FileAttachments) Len() int { return len(a.items) }
func (a FileAttachments) ByteSize() int {
	if len(a.items) == 0 {
		return 0
	}
	size := 1
	for _, item := range a.items {
		size += item.ByteSize() + 1
	}
	return size
}
func (a FileAttachments) Validate() error { _, err := NewFileAttachments(a.items); return err }

func (a FileAttachments) MarshalJSON() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if len(a.items) == 0 {
		return []byte("[]"), nil
	}
	return codec.MarshalStable(a.items)
}

// DecodeFileAttachments 验证非 null 的唯一当前数组。
func DecodeFileAttachments(data []byte) (FileAttachments, error) {
	if len(data) > MaxFileAttachmentsBytes+MaxFileReferences*256 || strings.TrimSpace(string(data)) == "null" {
		return FileAttachments{}, NewFileFailure(FileInvalid)
	}
	var items []FileAttachment
	if _, err := codec.CountArrayElements(data, MaxFileReferences); err != nil {
		return FileAttachments{}, NewFileFailure(FileInvalid)
	}
	if codec.UnmarshalStrict(data, &items) != nil || items == nil {
		return FileAttachments{}, NewFileFailure(FileInvalid)
	}
	return NewFileAttachments(items)
}

func (a *FileAttachments) UnmarshalJSON(data []byte) error {
	v, err := DecodeFileAttachments(data)
	if err == nil {
		*a = v
	}
	return err
}

package domain

import (
	"easycode/internal/codec"
	"slices"
)

const MaxFileSummaryBytes = 4 << 10

type fileSummaryItemFields struct {
	Path            string        `json:"path"`
	Selection       FileSelection `json:"selection"`
	TotalLines      uint64        `json:"total_lines"`
	ActualStartLine uint64        `json:"actual_start_line"`
	ActualEndLine   uint64        `json:"actual_end_line"`
	EndsMidLine     bool          `json:"ends_mid_line"`
	Truncated       bool          `json:"truncated"`
	RenderedBytes   int           `json:"rendered_bytes"`
}

// FileSummaryItem 只保留完整来源及实际范围，不能恢复附件正文。
type FileSummaryItem struct{ value fileSummaryItemFields }

func (v FileSummaryItem) Reference() FileReference {
	return FileReference{v.value.Path, v.value.Selection}
}
func (v FileSummaryItem) TotalLines() uint64      { return v.value.TotalLines }
func (v FileSummaryItem) ActualStartLine() uint64 { return v.value.ActualStartLine }
func (v FileSummaryItem) ActualEndLine() uint64   { return v.value.ActualEndLine }
func (v FileSummaryItem) EndsMidLine() bool       { return v.value.EndsMidLine }
func (v FileSummaryItem) Truncated() bool         { return v.value.Truncated }
func (v FileSummaryItem) RenderedBytes() int      { return v.value.RenderedBytes }

func (v FileSummaryItem) Validate() error {
	f := v.value
	if v.Reference().Validate() != nil || f.TotalLines > MaxFileSourceBytes || f.RenderedBytes < 1 || f.RenderedBytes > MaxFileRenderedBytes {
		return NewFileFailure(FileInvalid)
	}
	if f.TotalLines == 0 {
		if f.Selection.kind != "whole" || f.ActualStartLine != 0 || f.ActualEndLine != 0 || f.EndsMidLine || f.Truncated {
			return NewFileFailure(FileInvalid)
		}
	} else {
		first, last, cap := uint64(1), f.TotalLines, uint64(200)
		if f.Selection.kind == "lines" {
			first, last, cap = f.Selection.start, min(f.Selection.end, f.TotalLines), 2000
		}
		if f.ActualStartLine != first || f.ActualEndLine < first || f.ActualEndLine > last || f.ActualEndLine-first >= cap || f.Truncated != (f.ActualEndLine < last || f.EndsMidLine) {
			return NewFileFailure(FileInvalid)
		}
	}
	minimum, err := codec.MarshalStable(fileAttachmentContent{f.Path, f.Selection, f.TotalLines, f.ActualStartLine, f.ActualEndLine, f.EndsMidLine, f.Truncated, ""})
	if err != nil || f.RenderedBytes < len(fileAttachmentHeading)+len(minimum)+1 {
		return NewFileFailure(FileInvalid)
	}
	return nil
}
func (v FileSummaryItem) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(v.value)
}

// DecodeFileSummaryItem 只接受有界摘要字段，不允许夹带 content 或 digest。
func DecodeFileSummaryItem(data []byte) (FileSummaryItem, error) {
	var f fileSummaryItemFields
	if len(data) > MaxFileSummaryBytes || codec.DecodeObject(data, &f, "path", "selection", "total_lines", "actual_start_line", "actual_end_line", "ends_mid_line", "truncated", "rendered_bytes") != nil {
		return FileSummaryItem{}, NewFileFailure(FileInvalid)
	}
	v := FileSummaryItem{f}
	return v, v.Validate()
}
func (v *FileSummaryItem) UnmarshalJSON(data []byte) error {
	value, err := DecodeFileSummaryItem(data)
	if err == nil {
		*v = value
	}
	return err
}

type fileSummaryFields struct {
	Total   int               `json:"total"`
	Items   []FileSummaryItem `json:"items"`
	Omitted int               `json:"omitted"`
}

// FileSummary 私有持有编码后 4 KiB 内的完整项前缀；零值明确表示无附件。
type FileSummary struct {
	total   int
	items   []FileSummaryItem
	omitted int
}

func (v FileSummary) fields() fileSummaryFields {
	items := v.items
	if items == nil {
		items = []FileSummaryItem{}
	}
	return fileSummaryFields{v.total, items, v.omitted}
}
func (v FileSummary) Total() int               { return v.total }
func (v FileSummary) Items() []FileSummaryItem { return append([]FileSummaryItem(nil), v.items...) }
func (v FileSummary) Omitted() int             { return v.omitted }
func (v FileSummary) Clone() FileSummary       { v.items = v.Items(); return v }

// Equal 比较完整摘要值，不把 nil 与空集合当作不同的无附件语义。
func (v FileSummary) Equal(other FileSummary) bool {
	return v.total == other.total && v.omitted == other.omitted && slices.Equal(v.items, other.items)
}

// NewFileSummary 从完整冻结值纯派生；只省略完整尾项，从不截断实际路径。
func NewFileSummary(files FileAttachments) (FileSummary, error) {
	if err := files.Validate(); err != nil {
		return FileSummary{}, err
	}
	v := FileSummary{total: files.Len(), omitted: files.Len()}
	for _, a := range files.Items() {
		rendered, err := a.RenderedText()
		if err != nil {
			return FileSummary{}, err
		}
		item := FileSummaryItem{fileSummaryItemFields{a.Reference().Path(), a.Reference().Selection(), a.TotalLines(), a.ActualStartLine(), a.ActualEndLine(), a.EndsMidLine(), a.Truncated(), len(rendered)}}
		candidate := FileSummary{v.total, append(append([]FileSummaryItem(nil), v.items...), item), v.omitted - 1}
		data, err := codec.MarshalStable(candidate.fields())
		if err != nil {
			return FileSummary{}, err
		}
		if len(data) > MaxFileSummaryBytes {
			break
		}
		v = candidate
	}
	return v, v.Validate()
}
func (v FileSummary) Validate() error {
	if v.total < 0 || v.total > MaxFileReferences || v.omitted < 0 || len(v.items)+v.omitted != v.total {
		return NewFileFailure(FileInvalid)
	}
	for i, item := range v.items {
		if err := item.Validate(); err != nil {
			return err
		}
		for _, previous := range v.items[:i] {
			if previous.Reference() == item.Reference() {
				return NewFileFailure(FileInvalid)
			}
		}
	}
	data, err := codec.MarshalStable(v.fields())
	if err != nil || len(data) > MaxFileSummaryBytes {
		return NewFileFailure(FileLimit)
	}
	return nil
}
func (v FileSummary) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(v.fields())
}

// DecodeFileSummary 不把空或被省略的 items 当作完整附件事实。
func DecodeFileSummary(data []byte) (FileSummary, error) {
	var fields fileSummaryFields
	if len(data) > MaxFileSummaryBytes || codec.DecodeObject(data, &fields, "total", "items", "omitted") != nil || fields.Items == nil {
		return FileSummary{}, NewFileFailure(FileInvalid)
	}
	v := FileSummary{fields.Total, fields.Items, fields.Omitted}
	return v, v.Validate()
}
func (v *FileSummary) UnmarshalJSON(data []byte) error {
	value, err := DecodeFileSummary(data)
	if err == nil {
		*v = value
	}
	return err
}

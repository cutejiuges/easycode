package tool

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ArtifactReadFailure 是不含宿主路径或正文的范围读取错误。
type ArtifactReadFailure string

const (
	ArtifactQuotaExceeded    ArtifactReadFailure = "quota_exceeded"
	ArtifactReadUnauthorized ArtifactReadFailure = "unauthorized"
	ArtifactReadUnavailable  ArtifactReadFailure = "unavailable"
	ArtifactReadCorrupt      ArtifactReadFailure = "corrupt"
	ArtifactReadInvalidRange ArtifactReadFailure = "invalid_range"
)

func (failure ArtifactReadFailure) Error() string { return string(failure) }

// ArtifactRangeError 在完整字符无法放入范围时提供最小读取长度。
type ArtifactRangeError struct{ minimumLimit uint64 }

// NewArtifactRangeError 构造有界的字符范围错误。
func NewArtifactRangeError(minimumLimit uint64) (ArtifactRangeError, error) {
	if minimumLimit < 1 || minimumLimit > utf8.UTFMax {
		return ArtifactRangeError{}, errors.New("invalid minimum artifact limit")
	}
	return ArtifactRangeError{minimumLimit}, nil
}
func (e ArtifactRangeError) Error() string        { return "invalid_range" }
func (e ArtifactRangeError) Unwrap() error        { return ArtifactReadInvalidRange }
func (e ArtifactRangeError) MinimumLimit() uint64 { return e.minimumLimit }

// ArtifactPage 保存已通过完整性校验后的有界范围结果。
type ArtifactPage struct {
	id                   ArtifactID
	offset, next, stored uint64
	complete             bool
	text                 string
}

// NewArtifactPage 验证页范围与正文预算，不访问文件。
func NewArtifactPage(id ArtifactID, offset, next, stored uint64, complete bool, text string) (ArtifactPage, error) {
	p := ArtifactPage{id, offset, next, stored, complete, text}
	return p, p.Validate()
}
func (p ArtifactPage) ID() ArtifactID       { return p.id }
func (p ArtifactPage) Offset() uint64       { return p.offset }
func (p ArtifactPage) NextOffset() uint64   { return p.next }
func (p ArtifactPage) SavedBytes() uint64   { return p.stored }
func (p ArtifactPage) EOF() bool            { return p.next == p.stored }
func (p ArtifactPage) SourceComplete() bool { return p.complete }
func (p ArtifactPage) Text() string         { return p.text }

// Validate 拒绝无进度成功页、溢出与不可表示正文。
func (p ArtifactPage) Validate() error {
	if !p.id.Valid() || p.stored == 0 || p.stored > ArtifactStreamBytes || p.offset > p.next || p.next > p.stored || p.next-p.offset > ArtifactPageBytes || (p.offset == p.next && p.next != p.stored) || (p.offset == p.next && p.text != "") || !utf8.ValidString(p.text) || len(p.text) > MaxModelPreviewBytes-1024 {
		return errors.New("invalid artifact page")
	}
	if uint64(len(p.text)) < p.next-p.offset || uint64(len(p.text)) > 4*(p.next-p.offset) {
		return errors.New("invalid artifact page text size")
	}
	return nil
}

// Preview 将元数据与正文一起计入统一模型预算。
func (p ArtifactPage) Preview() (ModelPreview, error) {
	if err := p.Validate(); err != nil {
		return ModelPreview{}, err
	}
	return NewModelPreview(fmt.Sprintf("artifact_id: %s; offset: %d; next_offset: %d; saved_bytes: %d; stored_eof: %t; source_complete: %t\n%s", p.id, p.offset, p.next, p.stored, p.EOF(), p.complete, p.text))
}

// RenderArtifactRange 从包含前后最多三个边界字节的已校验窗口生成页。
// windowOffset 指定窗口首字节的原始偏移；完整性必须由调用者先验证。
func RenderArtifactRange(d ArtifactDescriptor, input ReadArtifactInput, windowOffset uint64, window []byte, allowance int) (ArtifactPage, error) {
	if allowance < SafetyResultBytes || allowance > MaxModelPreviewBytes {
		return ArtifactPage{}, ArtifactReadInvalidRange
	}
	if d.Validate() != nil || input.Validate() != nil || d.ID() != input.ID() {
		return ArtifactPage{}, ArtifactReadUnauthorized
	}
	size := uint64(d.SavedBytes())
	offset := input.Offset()
	if offset > size || windowOffset > offset || uint64(len(window)) > size-windowOffset {
		return ArtifactPage{}, ArtifactReadInvalidRange
	}
	start := offset - windowOffset
	if start > uint64(len(window)) {
		return ArtifactPage{}, ArtifactReadInvalidRange
	}
	// 从前缀探测有效多字节字符，拒绝从其内部开始；独立无效字节仍可转义。
	for back := uint64(1); back <= 3 && back <= start; back++ {
		_, n := utf8.DecodeRune(window[start-back:])
		if n > 1 && uint64(n) > back {
			return ArtifactPage{}, ArtifactReadInvalidRange
		}
	}
	end := min(size, offset+input.Limit())
	var text strings.Builder
	consumed := uint64(0)
	// 使用最大游标位数预留页头，正文始终取得正的可用空间。
	header := fmt.Sprintf("artifact_id: %s; offset: %d; next_offset: %d; saved_bytes: %d; stored_eof: false; source_complete: %t\n", d.ID(), offset, size, size, d.Complete())
	for offset+consumed < end {
		i := start + consumed
		if i >= uint64(len(window)) {
			return ArtifactPage{}, ArtifactReadInvalidRange
		}
		r, n := utf8.DecodeRune(window[i:])
		if offset+consumed+uint64(n) > end {
			if consumed == 0 {
				e, _ := NewArtifactRangeError(uint64(n))
				return ArtifactPage{}, e
			}
			break
		}
		var part string
		if r == utf8.RuneError && n == 1 {
			part = fmt.Sprintf("\\x%02X", window[i])
		} else {
			part = string(window[i : i+uint64(n)])
		}
		if text.Len()+len(part) > min(MaxModelPreviewBytes-1024, allowance-len(header)) {
			break
		}
		text.WriteString(part)
		consumed += uint64(n)
	}
	if consumed == 0 && offset < size {
		return ArtifactPage{}, ArtifactReadInvalidRange
	}
	return NewArtifactPage(d.ID(), offset, offset+consumed, size, d.Complete(), text.String())
}

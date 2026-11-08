package domain

import (
	"errors"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	MaxReasoningSourceBytes = 16 << 20
	MaxReasoningNodes       = 4096
)

// ReasoningSource 只标识允许展示的来源，不携带服务商原生身份。
type ReasoningSource string

const (
	ReasoningAnthropicThinking ReasoningSource = "anthropic_thinking"
	ReasoningOpenAISummary     ReasoningSource = "openai_summary"
)

// Valid 判断来源是否属于当前展示白名单。
func (s ReasoningSource) Valid() bool {
	return s == ReasoningAnthropicThinking || s == ReasoningOpenAISummary
}

// MatchesProvider 检查允许展示的来源与原生 Provider 一致。
func (s ReasoningSource) MatchesProvider(family ProviderFamily) bool {
	return s == ReasoningAnthropicThinking && family == ProviderAnthropic || s == ReasoningOpenAISummary && family == ProviderOpenAI
}

// ReasoningAvailability 区分允许正文与无正文的推理项。
type ReasoningAvailability string

const (
	ReasoningReadable    ReasoningAvailability = "readable"
	ReasoningUnavailable ReasoningAvailability = "unavailable"
)

// Valid 判断正文可用性是否属于当前契约。
func (a ReasoningAvailability) Valid() bool {
	return a == ReasoningReadable || a == ReasoningUnavailable
}

// ReasoningBody 持有不可变正文片段，段间双换行只在范围读取时生成。
type ReasoningBody struct {
	source ReasoningSource
	parts  []string
	bytes  uint64
}

type reasoningBodyWire struct {
	Source       ReasoningSource       `json:"source"`
	Availability ReasoningAvailability `json:"availability"`
	Parts        []string              `json:"parts"`
}

// NewReasoningBody 复制片段集合，忽略零字节段但保留其余全部空白。
func NewReasoningBody(source ReasoningSource, parts []string) (ReasoningBody, error) {
	if !source.Valid() || len(parts) > MaxReasoningNodes {
		return ReasoningBody{}, errors.New("invalid reasoning body")
	}
	var size uint64
	count := 0
	for _, part := range parts {
		if !utf8.ValidString(part) || len(part) > MaxReasoningSourceBytes {
			return ReasoningBody{}, errors.New("invalid reasoning text")
		}
		if part == "" {
			continue
		}
		if count > 0 {
			size += 2
		}
		size += uint64(len(part))
		count++
		if size > MaxReasoningSourceBytes {
			return ReasoningBody{}, errors.New("reasoning body exceeds size limit")
		}
	}
	if source == ReasoningAnthropicThinking && count > 1 {
		return ReasoningBody{}, errors.New("thinking body has multiple parts")
	}
	owned := make([]string, 0, count)
	for _, part := range parts {
		if part != "" {
			owned = append(owned, part)
		}
	}
	return ReasoningBody{source: source, parts: owned, bytes: size}, nil
}

// Source 返回经过白名单校验的来源。
func (b ReasoningBody) Source() ReasoningSource { return b.source }

// Availability 返回正文是否可读；零值仍须由 Validate 拒绝。
func (b ReasoningBody) Availability() ReasoningAvailability {
	if b.bytes == 0 {
		return ReasoningUnavailable
	}
	return ReasoningReadable
}

// Len 返回包含派生分隔符的正文原始字节数，不拼接全文。
func (b ReasoningBody) Len() uint64 { return b.bytes }

// Validate 校验不可变值是否已经由当前构造器创建。
func (b ReasoningBody) Validate() error {
	if !b.source.Valid() || b.parts == nil || b.bytes > MaxReasoningSourceBytes {
		return errors.New("invalid reasoning body")
	}
	return nil
}

// ReasoningRange 是一页独立正文，偏移基于原文而非终端显示列。
type ReasoningRange struct {
	Offset     uint64
	NextOffset uint64
	TotalBytes uint64
	Text       string
}

// ReadRange 按 UTF-8 边界读取一页，不为分页创建完整拼接正文。
func (b ReasoningBody) ReadRange(offset uint64, limit uint32, backward bool) (ReasoningRange, error) {
	if b.Validate() != nil || b.Availability() != ReasoningReadable || limit == 0 || limit > 64<<10 || offset > b.bytes || (offset < b.bytes && !utf8.RuneStart(b.byteAt(offset))) {
		return ReasoningRange{}, errors.New("invalid reasoning range")
	}
	start, end := offset, min(b.bytes, offset+uint64(limit))
	if backward {
		end = offset
		start = offset - min(offset, uint64(limit))
		for start < end && !utf8.RuneStart(b.byteAt(start)) {
			start++
		}
	}
	for end > start && end < b.bytes && !utf8.RuneStart(b.byteAt(end)) {
		end--
	}
	if start == end && end < b.bytes {
		return ReasoningRange{}, errors.New("reasoning range cannot hold a character")
	}
	var out strings.Builder
	out.Grow(int(end - start))
	position := uint64(0)
	appendSegment := func(segment string) {
		next := position + uint64(len(segment))
		if position < end && next > start {
			out.WriteString(segment[max(start, position)-position : min(end, next)-position])
		}
		position = next
	}
	for i, part := range b.parts {
		if i > 0 {
			appendSegment("\n\n")
		}
		appendSegment(part)
		if position >= end {
			break
		}
	}
	return ReasoningRange{Offset: start, NextOffset: end, TotalBytes: b.bytes, Text: out.String()}, nil
}

func (b ReasoningBody) byteAt(offset uint64) byte {
	for i, part := range b.parts {
		if i > 0 {
			if offset < 2 {
				return '\n'
			}
			offset -= 2
		}
		if offset < uint64(len(part)) {
			return part[offset]
		}
		offset -= uint64(len(part))
	}
	return 0
}

// MarshalJSON 仅编码允许正文，不读取外部资源。
func (b ReasoningBody) MarshalJSON() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return codec.MarshalStable(reasoningBodyWire{Source: b.source, Availability: b.Availability(), Parts: b.parts})
}

// UnmarshalJSON 对嵌套正文执行相同的唯一当前契约检查。
func (b *ReasoningBody) UnmarshalJSON(data []byte) error {
	v, err := DecodeReasoningBody(data)
	if err == nil {
		*b = v
	}
	return err
}

// DecodeReasoningBody 严格解码来源、可用性和片段，不接受私有扩展字段。
func DecodeReasoningBody(data []byte) (ReasoningBody, error) {
	var wire reasoningBodyWire
	if err := codec.DecodeObject(data, &wire, "source", "availability", "parts"); err != nil || wire.Parts == nil {
		return ReasoningBody{}, errors.New("invalid reasoning body encoding")
	}
	body, err := NewReasoningBody(wire.Source, wire.Parts)
	if err != nil || body.Availability() != wire.Availability || len(body.parts) != len(wire.Parts) {
		return ReasoningBody{}, errors.New("invalid reasoning body encoding")
	}
	return body, nil
}

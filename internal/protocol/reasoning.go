package protocol

import (
	"errors"
	"unicode/utf8"

	"easycode/internal/domain"
)

const (
	MaxReasoningDeltaBytes   = 16 << 10
	MaxReasoningPreviewBytes = 64 << 10
	MaxReasoningPayloadBytes = 512 << 10
)

// ReasoningStage 是当前推理块的封闭展示阶段，与持久化结果分离。
type ReasoningStage string

const (
	ReasoningStarted   ReasoningStage = "started"
	ReasoningDelta     ReasoningStage = "delta"
	ReasoningCompleted ReasoningStage = "completed"
)

// ReasoningDeltaPayload 保存已经确定顺序的原文范围。
type ReasoningDeltaPayload struct {
	Offset uint64 `json:"offset"`
	Text   string `json:"text"`
}

// ReasoningCompletion 保存有界最终快照，不能证明该 sample 已保存。
type ReasoningCompletion struct {
	Availability domain.ReasoningAvailability `json:"availability"`
	Offset       uint64                       `json:"offset"`
	TotalBytes   uint64                       `json:"total_bytes"`
	Text         string                       `json:"text"`
}

// AssistantReasoningPayload 只包含允许展示的推理，不包含原生身份。
type AssistantReasoningPayload struct {
	SampleIndex uint32                 `json:"sample_index"`
	Block       uint32                 `json:"block"`
	Source      domain.ReasoningSource `json:"source"`
	Stage       ReasoningStage         `json:"stage"`
	Delta       *ReasoningDeltaPayload `json:"delta,omitempty"`
	Completion  *ReasoningCompletion   `json:"completion,omitempty"`
}

// Validate 检查当前阶段的唯一字段组合和原文字节预算。
func (p AssistantReasoningPayload) Validate() error {
	if !p.Source.Valid() || p.Block >= domain.MaxReasoningNodes {
		return errors.New("invalid reasoning identity")
	}
	switch p.Stage {
	case ReasoningStarted:
		if p.Delta != nil || p.Completion != nil {
			return errors.New("unexpected reasoning start data")
		}
	case ReasoningDelta:
		if p.Delta == nil || p.Completion != nil || len(p.Delta.Text) == 0 || len(p.Delta.Text) > MaxReasoningDeltaBytes || !utf8.ValidString(p.Delta.Text) || p.Delta.Offset > domain.MaxReasoningSourceBytes || uint64(len(p.Delta.Text)) > domain.MaxReasoningSourceBytes-p.Delta.Offset {
			return errors.New("invalid reasoning delta")
		}
	case ReasoningCompleted:
		if p.Completion == nil || p.Delta != nil {
			return errors.New("invalid reasoning completion")
		}
		c := p.Completion
		if !c.Availability.Valid() || c.TotalBytes > domain.MaxReasoningSourceBytes || c.Offset > c.TotalBytes || uint64(len(c.Text)) != c.TotalBytes-c.Offset || len(c.Text) > MaxReasoningPreviewBytes || !utf8.ValidString(c.Text) {
			return errors.New("invalid reasoning completion range")
		}
		if c.Availability == domain.ReasoningUnavailable && (c.TotalBytes != 0 || c.Text != "") || c.Availability == domain.ReasoningReadable && (c.TotalBytes == 0 || c.Text == "") {
			return errors.New("invalid reasoning availability")
		}
	default:
		return errors.New("invalid reasoning stage")
	}
	return nil
}

// NewReasoningStarted 构造一个尚未绑定宿主 scope 的开始事件。
func NewReasoningStarted(block uint32, source domain.ReasoningSource) (Event, error) {
	return reasoningEvent(AssistantReasoningPayload{Block: block, Source: source, Stage: ReasoningStarted})
}

// NewReasoningDelta 构造一个已经排序且有界的文字增量。
func NewReasoningDelta(block uint32, source domain.ReasoningSource, offset uint64, text string) (Event, error) {
	return reasoningEvent(AssistantReasoningPayload{Block: block, Source: source, Stage: ReasoningDelta, Delta: &ReasoningDeltaPayload{Offset: offset, Text: text}})
}

// NewReasoningCompleted 构造替换快照，不将它作为新增量追加。
func NewReasoningCompleted(block uint32, source domain.ReasoningSource, completion ReasoningCompletion) (Event, error) {
	return reasoningEvent(AssistantReasoningPayload{Block: block, Source: source, Stage: ReasoningCompleted, Completion: &completion})
}

func reasoningEvent(p AssistantReasoningPayload) (Event, error) {
	if err := p.Validate(); err != nil {
		return Event{}, err
	}
	// 文字至多 64 KiB，即使每字节转义为六字节仍在 payload 硬限内。
	return newEventWithPayload(EventAssistantReasoning, p)
}

// DecodeAssistantReasoning 严格解码当前推理联合，拒绝超预算载荷。
func DecodeAssistantReasoning(event Event) (AssistantReasoningPayload, error) {
	if event.Kind != EventAssistantReasoning || len(event.Payload) > MaxReasoningPayloadBytes {
		return AssistantReasoningPayload{}, errors.New("invalid reasoning event")
	}
	return decodeTranscriptValue[AssistantReasoningPayload](event.Payload)
}

// BindReasoningSample 给 Provider 事件绑定当前真实采样序号。
func BindReasoningSample(event Event, sample uint32) (Event, error) {
	if event.SessionID != "" || event.ThreadID != "" || event.TurnID != "" {
		return Event{}, errors.New("reasoning event is already scoped")
	}
	p, err := DecodeAssistantReasoning(event)
	if err != nil || p.SampleIndex != 0 {
		return Event{}, errors.New("invalid unscoped reasoning event")
	}
	p.SampleIndex = sample
	bound, err := reasoningEvent(p)
	bound.Timestamp = event.Timestamp
	return bound, err
}

type reasoningBlockState struct {
	source domain.ReasoningSource
	bytes  uint64
	closed bool
}

// ReasoningStreamState 校验一个 sample 内的展示生命周期，不保存正文。
type ReasoningStreamState struct {
	blocks map[uint32]reasoningBlockState
	open   uint32
	sample uint32
}

// Accept 在成功校验后更新状态；失败不修改已有状态。
func (s *ReasoningStreamState) Accept(p AssistantReasoningPayload) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if len(s.blocks) > 0 && p.SampleIndex != s.sample {
		return errors.New("reasoning sample mismatch")
	}
	current, exists := s.blocks[p.Block]
	if p.Stage == ReasoningStarted {
		if exists || p.Block != uint32(len(s.blocks)) || len(s.blocks) >= domain.MaxReasoningNodes {
			return errors.New("reasoning start is duplicated or out of order")
		}
		if s.blocks == nil {
			s.blocks = make(map[uint32]reasoningBlockState)
			s.sample = p.SampleIndex
		}
		s.blocks[p.Block] = reasoningBlockState{source: p.Source}
		s.open++
		return nil
	}
	if !exists || current.closed || current.source != p.Source {
		return errors.New("reasoning block is unknown or closed")
	}
	if p.Stage == ReasoningDelta {
		if p.Delta.Offset != current.bytes {
			return errors.New("reasoning delta offset is not contiguous")
		}
		current.bytes += uint64(len(p.Delta.Text))
	} else {
		if p.Completion.TotalBytes < current.bytes {
			return errors.New("reasoning completion lost received text")
		}
		current.bytes, current.closed = p.Completion.TotalBytes, true
		s.open--
	}
	s.blocks[p.Block] = current
	return nil
}

// Complete 验证成功 sample 没有未关闭块，失败不会隐式关闭它们。
func (s *ReasoningStreamState) Complete() error {
	if s.open > 0 {
		return errors.New("reasoning sample has an open block")
	}
	return nil
}

// Pending 表示仍有尚未解析为提交或失败的块。
func (s *ReasoningStreamState) Pending() bool { return len(s.blocks) > 0 }

// Reset 在真实 sample 解析后释放所有块状态。
func (s *ReasoningStreamState) Reset() { *s = ReasoningStreamState{} }

// IsTransientPresentation 标识不会改变已提交上下文的高频展示事件。
func IsTransientPresentation(kind EventKind) bool {
	return kind == EventAssistantTextDelta || kind == EventAssistantReasoning
}

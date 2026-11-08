package protocol

import (
	"easycode/internal/domain"
	"errors"
)

// TranscriptCommitOrigin 明确区分输入与模型 sample 的提交。
type TranscriptCommitOrigin string

const (
	TranscriptCommitInput  TranscriptCommitOrigin = "input"
	TranscriptCommitSample TranscriptCommitOrigin = "sample"
)

// SampleCommitPayload 不含正文，仅将活动身份连接到原生位置。
type SampleCommitPayload struct {
	SampleIndex uint32                    `json:"sample_index"`
	ItemCount   uint32                    `json:"item_count"`
	Bindings    []domain.ReasoningBinding `json:"bindings"`
}

// Validate 在序列化和提交前验证当前绑定结构。
func (p SampleCommitPayload) Validate() error {
	if p.Bindings == nil {
		return errors.New("sample commit bindings are required")
	}
	_, err := domain.NewSamplePresentation(p.ItemCount, p.Bindings)
	return err
}

// TranscriptCommittedPayload 指明已确认的原生提交来源及展示位置。
type TranscriptCommittedPayload struct {
	Origin   TranscriptCommitOrigin `json:"origin"`
	Sequence uint64                 `json:"sequence"`
	Sample   *SampleCommitPayload   `json:"sample,omitempty"`
}

// Validate 拒绝含混来源、跨分支字段和超限绑定。
func (p TranscriptCommittedPayload) Validate() error {
	if p.Sequence == 0 {
		return errors.New("invalid transcript commit sequence")
	}
	switch p.Origin {
	case TranscriptCommitInput:
		if p.Sample != nil {
			return errors.New("input commit has sample data")
		}
	case TranscriptCommitSample:
		if p.Sample == nil || p.Sample.Validate() != nil {
			return errors.New("invalid sample commit data")
		}
	default:
		return errors.New("invalid transcript commit origin")
	}
	return nil
}

// NewInputTranscriptCommitted 构造不解析活动 sample 的输入提交边界。
func NewInputTranscriptCommitted(sequence uint64) (Event, error) {
	p := TranscriptCommittedPayload{Origin: TranscriptCommitInput, Sequence: sequence}
	if err := p.Validate(); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventTranscriptCommitted, p)
}

func sampleCommitPayload(sequence uint64, index uint32, presentation domain.SamplePresentation) (TranscriptCommittedPayload, error) {
	if presentation.Validate() != nil {
		return TranscriptCommittedPayload{}, errors.New("invalid sample presentation")
	}
	p := TranscriptCommittedPayload{Origin: TranscriptCommitSample, Sequence: sequence, Sample: &SampleCommitPayload{SampleIndex: index, ItemCount: presentation.ItemCount(), Bindings: presentation.Bindings()}}
	return p, p.Validate()
}

// NewSampleTranscriptCommitted 构造已保存 sample 的独立位置映射。
func NewSampleTranscriptCommitted(sequence uint64, index uint32, presentation domain.SamplePresentation) (Event, error) {
	p, err := sampleCommitPayload(sequence, index, presentation)
	if err != nil {
		return Event{}, err
	}
	event, err := newEventWithPayload(EventTranscriptCommitted, p)
	if err == nil && len(event.Payload) > MaxReasoningPayloadBytes {
		return Event{}, errors.New("sample commit payload exceeds limit")
	}
	return event, err
}

// DecodeTranscriptCommitted 严格解码当前来源联合，不接受旧的纯 sequence 结构。
func DecodeTranscriptCommitted(event Event) (TranscriptCommittedPayload, error) {
	if event.Kind != EventTranscriptCommitted || len(event.Payload) > MaxReasoningPayloadBytes {
		return TranscriptCommittedPayload{}, errors.New("invalid transcript commit event")
	}
	return decodeTranscriptValue[TranscriptCommittedPayload](event.Payload)
}

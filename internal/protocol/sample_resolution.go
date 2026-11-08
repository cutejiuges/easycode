package protocol

import (
	"easycode/internal/codec"
	"errors"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
)

// SampleDisposition 记录实际提交结果，不由取消时刻推测。
type SampleDisposition string

const (
	SampleNotCommitted SampleDisposition = "not_committed"
	SampleDurable      SampleDisposition = "durable"
	SampleUncertain    SampleDisposition = "uncertain"
)

// SampleResolution 只描述确有当前 sample 时的失败解析。
type SampleResolution struct {
	SampleIndex uint32                      `json:"sample_index"`
	Disposition SampleDisposition           `json:"disposition"`
	Committed   *TranscriptCommittedPayload `json:"committed,omitempty"`
}

// NewUncommittedSampleResolution 表达确定未提交的当前 sample。
func NewUncommittedSampleResolution(index uint32) SampleResolution {
	return SampleResolution{SampleIndex: index, Disposition: SampleNotCommitted}
}

// NewUncertainSampleResolution 表达不能确认保存结果的当前 sample。
func NewUncertainSampleResolution(index uint32) SampleResolution {
	return SampleResolution{SampleIndex: index, Disposition: SampleUncertain}
}

// NewDurableSampleResolution 表达保存后发生的本地 finalize 或投影失败。
func NewDurableSampleResolution(index uint32, sequence uint64, presentation domain.SamplePresentation) (SampleResolution, error) {
	commit, err := sampleCommitPayload(sequence, index, presentation)
	if err != nil {
		return SampleResolution{}, err
	}
	return SampleResolution{SampleIndex: index, Disposition: SampleDurable, Committed: &commit}, nil
}

// Validate 拒绝未知结果、伪造位置与跨 sample 的确定提交。
func (r SampleResolution) Validate() error {
	switch r.Disposition {
	case SampleNotCommitted, SampleUncertain:
		if r.Committed != nil {
			return errors.New("unconfirmed sample has committed identity")
		}
	case SampleDurable:
		if r.Committed == nil || r.Committed.Validate() != nil || r.Committed.Origin != TranscriptCommitSample || r.Committed.Sample.SampleIndex != r.SampleIndex {
			return errors.New("invalid durable sample resolution")
		}
	default:
		return errors.New("invalid sample disposition")
	}
	return nil
}

// NewTurnSampleFailed 与无当前 sample 的失败构造分离，不改变 Session 失败记录。
func NewTurnSampleFailed(code, message string, cancelled bool, resolution SampleResolution) (Event, error) {
	if err := resolution.Validate(); err != nil {
		return Event{}, err
	}
	if strings.TrimSpace(code) == "" || strings.TrimSpace(message) == "" || !utf8.ValidString(code) || !utf8.ValidString(message) {
		return Event{}, errors.New("invalid sample failure message")
	}
	base, err := codec.MarshalStable(TurnFailedPayload{Cancelled: cancelled, Resolution: &resolution})
	if err != nil {
		return Event{}, err
	}
	if len(base) > MaxReasoningPayloadBytes || jsonStringContentBytes(code)+jsonStringContentBytes(message) > MaxReasoningPayloadBytes-len(base) {
		return Event{}, errors.New("sample failure payload exceeds limit")
	}

	event, err := newEventWithPayload(EventTurnFailed, TurnFailedPayload{Code: code, Message: message, Cancelled: cancelled, Resolution: &resolution})
	if err == nil && len(event.Payload) > MaxReasoningPayloadBytes {
		return Event{}, errors.New("sample failure payload exceeds limit")
	}
	return event, err
}

// Commit 核对已关闭活动块与确定位置，然后释放当前 sample 状态。
func (s *ReasoningStreamState) Commit(p SampleCommitPayload) error {
	if p.Validate() != nil || s.Complete() != nil || len(p.Bindings) != len(s.blocks) || len(s.blocks) > 0 && p.SampleIndex != s.sample {
		return errors.New("reasoning sample commit mismatch")
	}
	for _, binding := range p.Bindings {
		block, exists := s.blocks[binding.Block]
		availability := domain.ReasoningUnavailable
		if block.bytes > 0 {
			availability = domain.ReasoningReadable
		}
		if !exists || block.source != binding.Source || availability != binding.Availability {
			return errors.New("reasoning binding does not match its block")
		}
	}
	s.Reset()
	return nil
}

// Resolve 用真实失败事实关闭当前全部活动块，不要求逐块 Completed。
func (s *ReasoningStreamState) Resolve(r SampleResolution) error {
	if r.Validate() != nil || len(s.blocks) > 0 && r.SampleIndex != s.sample {
		return errors.New("reasoning failure sample mismatch")
	}
	if r.Disposition == SampleDurable {
		return s.Commit(*r.Committed.Sample)
	}
	s.Reset()
	return nil
}

// jsonStringContentBytes 与固定 codec 的 HTML/控制字符转义口径一致，不分配正文副本。
func jsonStringContentBytes(text string) int {
	size := 0
	for _, r := range text {
		switch {
		case r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '\t':
			size += 2
		case r < 0x20 || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029':
			size += 6
		default:
			size += utf8.RuneLen(r)
		}
		if size > MaxReasoningPayloadBytes {
			return size
		}
	}
	return size
}

package protocol

import "errors"

// TranscriptCommittedPayload 指明宿主投影已更新的 durable 原生提交位置。
type TranscriptCommittedPayload struct {
	Sequence uint64 `json:"sequence"`
}

func (p TranscriptCommittedPayload) Validate() error {
	if p.Sequence == 0 {
		return errors.New("invalid transcript commit sequence")
	}
	return nil
}

// NewTranscriptCommitted 不携带正文或 Provider 原生字段。
func NewTranscriptCommitted(sequence uint64) (Event, error) {
	p := TranscriptCommittedPayload{Sequence: sequence}
	if err := p.Validate(); err != nil {
		return Event{}, err
	}
	return newEventWithPayload(EventTranscriptCommitted, p)
}
func DecodeTranscriptCommitted(event Event) (TranscriptCommittedPayload, error) {
	if event.Kind != EventTranscriptCommitted {
		return TranscriptCommittedPayload{}, errors.New("invalid transcript commit kind")
	}
	return decodeTranscriptValue[TranscriptCommittedPayload](event.Payload)
}

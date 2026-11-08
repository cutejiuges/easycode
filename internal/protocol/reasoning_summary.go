package protocol

import (
	"errors"

	"easycode/internal/domain"
)

// ReasoningSummaryPayload 是列表可携带的有界元数据，不包含原生字段。
type ReasoningSummaryPayload struct {
	Source       domain.ReasoningSource       `json:"source"`
	Availability domain.ReasoningAvailability `json:"availability"`
	TotalBytes   uint64                       `json:"total_bytes"`
}

// Validate 确认来源及正文长度与可用性相符。
func (p ReasoningSummaryPayload) Validate() error {
	if !p.Source.Valid() || !p.Availability.Valid() || p.TotalBytes > domain.MaxReasoningSourceBytes || (p.Availability == domain.ReasoningReadable) != (p.TotalBytes > 0) {
		return errors.New("invalid reasoning summary")
	}
	return nil
}

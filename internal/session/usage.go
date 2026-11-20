package session

import (
	"fmt"

	"easycode/internal/domain"
)

// UsageMetricPayload 是当前 Session 中显式区分状态和值的指标 DTO。
type UsageMetricPayload struct {
	State domain.UsageMetricState `json:"state"`
	Value *uint64                 `json:"value,omitempty"`
}

// SampleUsagePayload 是当前 sample_usage 的强类型 payload。
type SampleUsagePayload struct {
	InputUncached   UsageMetricPayload `json:"input_uncached"`
	CacheRead       UsageMetricPayload `json:"cache_read"`
	CacheWrite      UsageMetricPayload `json:"cache_write"`
	Output          UsageMetricPayload `json:"output"`
	ReasoningOutput UsageMetricPayload `json:"reasoning_output"`
}

// NewSampleUsagePayload 从领域值创建 Session wire DTO。
func NewSampleUsagePayload(usage domain.SampleUsage) (SampleUsagePayload, error) {
	if err := usage.Validate(); err != nil {
		return SampleUsagePayload{}, fmt.Errorf("sample usage is invalid: %w", err)
	}
	fields := usage.Fields()
	return SampleUsagePayload{
		InputUncached:   UsageMetricPayload(fields.InputUncached),
		CacheRead:       UsageMetricPayload(fields.CacheRead),
		CacheWrite:      UsageMetricPayload(fields.CacheWrite),
		Output:          UsageMetricPayload(fields.Output),
		ReasoningOutput: UsageMetricPayload(fields.ReasoningOutput),
	}, nil
}

// Domain 校验 DTO 并转换为不可变领域值。
func (payload SampleUsagePayload) Domain() (domain.SampleUsage, error) {
	return domain.NewSampleUsageFromFields(domain.SampleUsageFields{
		InputUncached:   domain.UsageMetricFields(payload.InputUncached),
		CacheRead:       domain.UsageMetricFields(payload.CacheRead),
		CacheWrite:      domain.UsageMetricFields(payload.CacheWrite),
		Output:          domain.UsageMetricFields(payload.Output),
		ReasoningOutput: domain.UsageMetricFields(payload.ReasoningOutput),
	})
}

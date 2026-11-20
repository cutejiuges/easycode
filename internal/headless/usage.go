package headless

import (
	"fmt"

	"easycode/internal/domain"
)

// UsageMetric 是 headless JSONL v1 中显式区分状态和值的指标。
type UsageMetric struct {
	State domain.UsageMetricState `json:"state"`
	Value *uint64                 `json:"value,omitempty"`
}

// Usage 是 turn.completed v1 发布的五项归一化指标。
type Usage struct {
	InputUncached   UsageMetric `json:"input_uncached"`
	CacheRead       UsageMetric `json:"cache_read"`
	CacheWrite      UsageMetric `json:"cache_write"`
	Output          UsageMetric `json:"output"`
	ReasoningOutput UsageMetric `json:"reasoning_output"`
}

func newUsage(usage domain.SampleUsage) (Usage, error) {
	if err := usage.Validate(); err != nil {
		return Usage{}, fmt.Errorf("headless usage is invalid: %w", err)
	}
	fields := usage.Fields()
	return Usage{
		InputUncached:   UsageMetric(fields.InputUncached),
		CacheRead:       UsageMetric(fields.CacheRead),
		CacheWrite:      UsageMetric(fields.CacheWrite),
		Output:          UsageMetric(fields.Output),
		ReasoningOutput: UsageMetric(fields.ReasoningOutput),
	}, nil
}

// Domain 校验 DTO 并转换为不可变领域值。
func (payload Usage) Domain() (domain.SampleUsage, error) {
	return domain.NewSampleUsageFromFields(domain.SampleUsageFields{
		InputUncached:   domain.UsageMetricFields(payload.InputUncached),
		CacheRead:       domain.UsageMetricFields(payload.CacheRead),
		CacheWrite:      domain.UsageMetricFields(payload.CacheWrite),
		Output:          domain.UsageMetricFields(payload.Output),
		ReasoningOutput: domain.UsageMetricFields(payload.ReasoningOutput),
	})
}

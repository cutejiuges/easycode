package protocol

import (
	"fmt"

	"easycode/internal/domain"
)

// UsageMetricPayload 是 Runtime event v1 中显式区分状态和值的指标 DTO。
type UsageMetricPayload struct {
	State domain.UsageMetricState `json:"state"`
	Value *uint64                 `json:"value,omitempty"`
}

// UsagePayload 是当前 turn_completed 事件发布的 turn usage。
type UsagePayload struct {
	InputUncached   UsageMetricPayload `json:"input_uncached"`
	CacheRead       UsageMetricPayload `json:"cache_read"`
	CacheWrite      UsageMetricPayload `json:"cache_write"`
	Output          UsageMetricPayload `json:"output"`
	ReasoningOutput UsageMetricPayload `json:"reasoning_output"`
}

// NewUsagePayload 创建不含 Provider 原生字段的计量投影。
func NewUsagePayload(usage domain.SampleUsage) (UsagePayload, error) {
	if err := usage.Validate(); err != nil {
		return UsagePayload{}, fmt.Errorf("turn usage is invalid: %w", err)
	}
	fields := usage.Fields()
	return UsagePayload{
		InputUncached:   UsageMetricPayload(fields.InputUncached),
		CacheRead:       UsageMetricPayload(fields.CacheRead),
		CacheWrite:      UsageMetricPayload(fields.CacheWrite),
		Output:          UsageMetricPayload(fields.Output),
		ReasoningOutput: UsageMetricPayload(fields.ReasoningOutput),
	}, nil
}

// Domain 校验 DTO 并转换为不可变领域值。
func (payload UsagePayload) Domain() (domain.SampleUsage, error) {
	return domain.NewSampleUsageFromFields(domain.SampleUsageFields{
		InputUncached:   domain.UsageMetricFields(payload.InputUncached),
		CacheRead:       domain.UsageMetricFields(payload.CacheRead),
		CacheWrite:      domain.UsageMetricFields(payload.CacheWrite),
		Output:          domain.UsageMetricFields(payload.Output),
		ReasoningOutput: domain.UsageMetricFields(payload.ReasoningOutput),
	})
}

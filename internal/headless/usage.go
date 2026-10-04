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
	return Usage{
		InputUncached:   newUsageMetric(usage.InputUncached()),
		CacheRead:       newUsageMetric(usage.CacheRead()),
		CacheWrite:      newUsageMetric(usage.CacheWrite()),
		Output:          newUsageMetric(usage.Output()),
		ReasoningOutput: newUsageMetric(usage.ReasoningOutput()),
	}, nil
}

// Domain 校验 DTO 并转换为不可变领域值。
func (usage Usage) Domain() (domain.SampleUsage, error) {
	inputUncached, err := usage.InputUncached.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("input_uncached metric is invalid: %w", err)
	}
	cacheRead, err := usage.CacheRead.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("cache_read metric is invalid: %w", err)
	}
	cacheWrite, err := usage.CacheWrite.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("cache_write metric is invalid: %w", err)
	}
	output, err := usage.Output.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("output metric is invalid: %w", err)
	}
	reasoningOutput, err := usage.ReasoningOutput.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("reasoning_output metric is invalid: %w", err)
	}
	return domain.NewSampleUsage(inputUncached, cacheRead, cacheWrite, output, reasoningOutput)
}

func newUsageMetric(metric domain.UsageMetric) UsageMetric {
	payload := UsageMetric{State: metric.State()}
	if value, known := metric.Value(); known {
		payload.Value = &value
	}
	return payload
}

func (metric UsageMetric) domain() (domain.UsageMetric, error) {
	switch metric.State {
	case domain.UsageMetricKnown:
		if metric.Value == nil {
			return domain.UsageMetric{}, fmt.Errorf("known usage metric requires a value")
		}
		return domain.KnownUsageMetric(*metric.Value), nil
	case domain.UsageMetricUnknown:
		if metric.Value != nil {
			return domain.UsageMetric{}, fmt.Errorf("unknown usage metric must not contain a value")
		}
		return domain.UnknownUsageMetric(), nil
	case domain.UsageMetricNotApplicable:
		if metric.Value != nil {
			return domain.UsageMetric{}, fmt.Errorf("not_applicable usage metric must not contain a value")
		}
		return domain.NotApplicableUsageMetric(), nil
	default:
		return domain.UsageMetric{}, fmt.Errorf("usage metric state is invalid")
	}
}

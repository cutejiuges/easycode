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

// UsagePayload 是 turn_completed v1 对外发布的 turn usage。
type UsagePayload struct {
	InputUncached   UsageMetricPayload `json:"input_uncached"`
	CacheRead       UsageMetricPayload `json:"cache_read"`
	CacheWrite      UsageMetricPayload `json:"cache_write"`
	Output          UsageMetricPayload `json:"output"`
	ReasoningOutput UsageMetricPayload `json:"reasoning_output"`
}

func newUsagePayload(usage domain.SampleUsage) (UsagePayload, error) {
	if err := usage.Validate(); err != nil {
		return UsagePayload{}, fmt.Errorf("turn usage is invalid: %w", err)
	}
	return UsagePayload{
		InputUncached:   newUsageMetricPayload(usage.InputUncached()),
		CacheRead:       newUsageMetricPayload(usage.CacheRead()),
		CacheWrite:      newUsageMetricPayload(usage.CacheWrite()),
		Output:          newUsageMetricPayload(usage.Output()),
		ReasoningOutput: newUsageMetricPayload(usage.ReasoningOutput()),
	}, nil
}

// Domain 校验 DTO 并转换为不可变领域值。
func (payload UsagePayload) Domain() (domain.SampleUsage, error) {
	inputUncached, err := payload.InputUncached.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("input_uncached metric is invalid: %w", err)
	}
	cacheRead, err := payload.CacheRead.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("cache_read metric is invalid: %w", err)
	}
	cacheWrite, err := payload.CacheWrite.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("cache_write metric is invalid: %w", err)
	}
	output, err := payload.Output.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("output metric is invalid: %w", err)
	}
	reasoningOutput, err := payload.ReasoningOutput.domain()
	if err != nil {
		return domain.SampleUsage{}, fmt.Errorf("reasoning_output metric is invalid: %w", err)
	}
	return domain.NewSampleUsage(inputUncached, cacheRead, cacheWrite, output, reasoningOutput)
}

func newUsageMetricPayload(metric domain.UsageMetric) UsageMetricPayload {
	payload := UsageMetricPayload{State: metric.State()}
	if value, known := metric.Value(); known {
		payload.Value = &value
	}
	return payload
}

func (payload UsageMetricPayload) domain() (domain.UsageMetric, error) {
	switch payload.State {
	case domain.UsageMetricKnown:
		if payload.Value == nil {
			return domain.UsageMetric{}, fmt.Errorf("known usage metric requires a value")
		}
		return domain.KnownUsageMetric(*payload.Value), nil
	case domain.UsageMetricUnknown:
		if payload.Value != nil {
			return domain.UsageMetric{}, fmt.Errorf("unknown usage metric must not contain a value")
		}
		return domain.UnknownUsageMetric(), nil
	case domain.UsageMetricNotApplicable:
		if payload.Value != nil {
			return domain.UsageMetric{}, fmt.Errorf("not_applicable usage metric must not contain a value")
		}
		return domain.NotApplicableUsageMetric(), nil
	default:
		return domain.UsageMetric{}, fmt.Errorf("usage metric state is invalid")
	}
}

package domain

import (
	"fmt"
	"math"
)

// UsageMetricState 表示一个 token 指标的观测状态。
type UsageMetricState string

const (
	UsageMetricKnown         UsageMetricState = "known"
	UsageMetricUnknown       UsageMetricState = "unknown"
	UsageMetricNotApplicable UsageMetricState = "not_applicable"
)

// UsageMetric 表示一个不可变的 token 指标。
type UsageMetric struct {
	state UsageMetricState
	value uint64
}

// KnownUsageMetric 创建 Provider 明确报告或可无损推导的指标。
func KnownUsageMetric(value uint64) UsageMetric {
	return UsageMetric{state: UsageMetricKnown, value: value}
}

// UnknownUsageMetric 创建 Provider 能表达但本次没有足够信息的指标。
func UnknownUsageMetric() UsageMetric {
	return UsageMetric{state: UsageMetricUnknown}
}

// NotApplicableUsageMetric 创建 Provider wire 不存在该独立概念的指标。
func NotApplicableUsageMetric() UsageMetric {
	return UsageMetric{state: UsageMetricNotApplicable}
}

// State 返回指标状态。
func (metric UsageMetric) State() UsageMetricState {
	return metric.state
}

// Value 返回 known 指标值；其他状态返回 false。
func (metric UsageMetric) Value() (uint64, bool) {
	return metric.value, metric.state == UsageMetricKnown
}

// Validate 校验指标状态和值的不变量。
func (metric UsageMetric) Validate() error {
	switch metric.state {
	case UsageMetricKnown:
		return nil
	case UsageMetricUnknown, UsageMetricNotApplicable:
		if metric.value != 0 {
			return fmt.Errorf("usage metric without a known state contains a value")
		}
		return nil
	default:
		return fmt.Errorf("usage metric state is invalid")
	}
}

// SampleUsage 保存一次 Provider sample 的归一化 token 用量。
type SampleUsage struct {
	inputUncached   UsageMetric
	cacheRead       UsageMetric
	cacheWrite      UsageMetric
	output          UsageMetric
	reasoningOutput UsageMetric
}

// NewSampleUsage 创建并校验不可变 sample usage。
func NewSampleUsage(
	inputUncached UsageMetric,
	cacheRead UsageMetric,
	cacheWrite UsageMetric,
	output UsageMetric,
	reasoningOutput UsageMetric,
) (SampleUsage, error) {
	usage := SampleUsage{
		inputUncached: inputUncached, cacheRead: cacheRead, cacheWrite: cacheWrite,
		output: output, reasoningOutput: reasoningOutput,
	}
	if err := usage.Validate(); err != nil {
		return SampleUsage{}, err
	}
	return usage, nil
}

// InputUncached 返回未命中缓存的输入 token。
func (usage SampleUsage) InputUncached() UsageMetric { return usage.inputUncached }

// CacheRead 返回缓存读取 token。
func (usage SampleUsage) CacheRead() UsageMetric { return usage.cacheRead }

// CacheWrite 返回缓存写入 token。
func (usage SampleUsage) CacheWrite() UsageMetric { return usage.cacheWrite }

// Output 返回输出 token。
func (usage SampleUsage) Output() UsageMetric { return usage.output }

// ReasoningOutput 返回 reasoning 输出 token。
func (usage SampleUsage) ReasoningOutput() UsageMetric { return usage.reasoningOutput }

// Validate 校验全部指标。
func (usage SampleUsage) Validate() error {
	metrics := [...]struct {
		name   string
		metric UsageMetric
	}{
		{name: "input_uncached", metric: usage.inputUncached},
		{name: "cache_read", metric: usage.cacheRead},
		{name: "cache_write", metric: usage.cacheWrite},
		{name: "output", metric: usage.output},
		{name: "reasoning_output", metric: usage.reasoningOutput},
	}
	for _, candidate := range metrics {
		if err := candidate.metric.Validate(); err != nil {
			return fmt.Errorf("%s metric is invalid: %w", candidate.name, err)
		}
	}
	return nil
}

// AggregateSampleUsage 按顺序聚合一个非空 sample 集合。
func AggregateSampleUsage(samples []SampleUsage) (SampleUsage, error) {
	if len(samples) == 0 {
		return SampleUsage{}, fmt.Errorf("sample usage collection is empty")
	}
	for _, sample := range samples {
		if err := sample.Validate(); err != nil {
			return SampleUsage{}, fmt.Errorf("sample usage is invalid: %w", err)
		}
	}
	inputUncached, err := aggregateUsageMetric(samples, func(usage SampleUsage) UsageMetric { return usage.inputUncached })
	if err != nil {
		return SampleUsage{}, fmt.Errorf("aggregate input_uncached: %w", err)
	}
	cacheRead, err := aggregateUsageMetric(samples, func(usage SampleUsage) UsageMetric { return usage.cacheRead })
	if err != nil {
		return SampleUsage{}, fmt.Errorf("aggregate cache_read: %w", err)
	}
	cacheWrite, err := aggregateUsageMetric(samples, func(usage SampleUsage) UsageMetric { return usage.cacheWrite })
	if err != nil {
		return SampleUsage{}, fmt.Errorf("aggregate cache_write: %w", err)
	}
	output, err := aggregateUsageMetric(samples, func(usage SampleUsage) UsageMetric { return usage.output })
	if err != nil {
		return SampleUsage{}, fmt.Errorf("aggregate output: %w", err)
	}
	reasoningOutput, err := aggregateUsageMetric(samples, func(usage SampleUsage) UsageMetric { return usage.reasoningOutput })
	if err != nil {
		return SampleUsage{}, fmt.Errorf("aggregate reasoning_output: %w", err)
	}
	return NewSampleUsage(inputUncached, cacheRead, cacheWrite, output, reasoningOutput)
}

func aggregateUsageMetric(samples []SampleUsage, selectMetric func(SampleUsage) UsageMetric) (UsageMetric, error) {
	for _, sample := range samples {
		metric := selectMetric(sample)
		switch metric.state {
		case UsageMetricUnknown:
			return UnknownUsageMetric(), nil
		case UsageMetricKnown, UsageMetricNotApplicable:
		default:
			return UsageMetric{}, fmt.Errorf("usage metric state is invalid")
		}
	}

	var total uint64
	known := false
	for _, sample := range samples {
		metric := selectMetric(sample)
		if metric.state != UsageMetricKnown {
			continue
		}
		known = true
		if metric.value > math.MaxUint64-total {
			return UsageMetric{}, fmt.Errorf("usage metric overflow")
		}
		total += metric.value
	}
	if !known {
		return NotApplicableUsageMetric(), nil
	}
	return KnownUsageMetric(total), nil
}

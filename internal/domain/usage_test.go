package domain

import (
	"math"
	"testing"
)

func TestAggregateSampleUsageUnknownDominatesOverflowRegardlessOfOrder(t *testing.T) {
	t.Parallel()
	maximum := mustSampleUsage(t,
		KnownUsageMetric(math.MaxUint64), NotApplicableUsageMetric(), NotApplicableUsageMetric(),
		NotApplicableUsageMetric(), NotApplicableUsageMetric(),
	)
	one := mustSampleUsage(t,
		KnownUsageMetric(1), NotApplicableUsageMetric(), NotApplicableUsageMetric(),
		NotApplicableUsageMetric(), NotApplicableUsageMetric(),
	)
	unknown := mustSampleUsage(t,
		UnknownUsageMetric(), NotApplicableUsageMetric(), NotApplicableUsageMetric(),
		NotApplicableUsageMetric(), NotApplicableUsageMetric(),
	)
	aggregated, err := AggregateSampleUsage([]SampleUsage{maximum, one, unknown})
	if err != nil {
		t.Fatal(err)
	}
	assertUsageMetric(t, aggregated.InputUncached(), UsageMetricUnknown, 0, false)
}

func mustSampleUsage(t *testing.T, metrics ...UsageMetric) SampleUsage {
	t.Helper()
	usage, err := NewSampleUsage(metrics[0], metrics[1], metrics[2], metrics[3], metrics[4])
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

func assertUsageMetric(t *testing.T, metric UsageMetric, state UsageMetricState, value uint64, hasValue bool) {
	t.Helper()
	got, ok := metric.Value()
	if metric.State() != state || got != value || ok != hasValue {
		t.Fatalf("metric = state %q value %d ok %t, want state %q value %d ok %t", metric.State(), got, ok, state, value, hasValue)
	}
}

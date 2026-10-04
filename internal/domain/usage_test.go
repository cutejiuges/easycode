package domain

import (
	"math"
	"testing"
)

func TestUsageMetricStatesPreserveKnownZero(t *testing.T) {
	t.Parallel()
	known := KnownUsageMetric(0)
	if err := known.Validate(); err != nil {
		t.Fatal(err)
	}
	if value, ok := known.Value(); !ok || value != 0 || known.State() != UsageMetricKnown {
		t.Fatalf("known zero = state %q value %d ok %t", known.State(), value, ok)
	}
	for _, metric := range []UsageMetric{UnknownUsageMetric(), NotApplicableUsageMetric()} {
		if err := metric.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, ok := metric.Value(); ok {
			t.Fatalf("metric %q unexpectedly has a value", metric.State())
		}
	}
	if err := (UsageMetric{}).Validate(); err == nil {
		t.Fatal("zero-value metric unexpectedly passed validation")
	}
}

func TestSampleUsageRejectsInvalidMetric(t *testing.T) {
	t.Parallel()
	if _, err := NewSampleUsage(
		UsageMetric{}, UnknownUsageMetric(), UnknownUsageMetric(), UnknownUsageMetric(), UnknownUsageMetric(),
	); err == nil {
		t.Fatal("invalid metric unexpectedly accepted")
	}
	if err := (SampleUsage{}).Validate(); err == nil {
		t.Fatal("zero-value usage unexpectedly passed validation")
	}
}

func TestAggregateSampleUsageStateAlgebra(t *testing.T) {
	t.Parallel()
	first := mustSampleUsage(t,
		KnownUsageMetric(10), KnownUsageMetric(7), NotApplicableUsageMetric(),
		KnownUsageMetric(3), NotApplicableUsageMetric(),
	)
	second := mustSampleUsage(t,
		KnownUsageMetric(4), NotApplicableUsageMetric(), NotApplicableUsageMetric(),
		UnknownUsageMetric(), NotApplicableUsageMetric(),
	)
	aggregated, err := AggregateSampleUsage([]SampleUsage{first, second})
	if err != nil {
		t.Fatal(err)
	}
	assertUsageMetric(t, aggregated.InputUncached(), UsageMetricKnown, 14, true)
	assertUsageMetric(t, aggregated.CacheRead(), UsageMetricKnown, 7, true)
	assertUsageMetric(t, aggregated.CacheWrite(), UsageMetricNotApplicable, 0, false)
	assertUsageMetric(t, aggregated.Output(), UsageMetricUnknown, 0, false)
	assertUsageMetric(t, aggregated.ReasoningOutput(), UsageMetricNotApplicable, 0, false)
}

func TestAggregateSampleUsageRejectsEmptyAndOverflow(t *testing.T) {
	t.Parallel()
	if _, err := AggregateSampleUsage(nil); err == nil {
		t.Fatal("empty collection unexpectedly aggregated")
	}
	first := mustSampleUsage(t,
		KnownUsageMetric(math.MaxUint64), UnknownUsageMetric(), UnknownUsageMetric(),
		UnknownUsageMetric(), UnknownUsageMetric(),
	)
	second := mustSampleUsage(t,
		KnownUsageMetric(1), UnknownUsageMetric(), UnknownUsageMetric(),
		UnknownUsageMetric(), UnknownUsageMetric(),
	)
	if _, err := AggregateSampleUsage([]SampleUsage{first, second}); err == nil {
		t.Fatal("overflow unexpectedly aggregated")
	}
}

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

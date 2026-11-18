package provider

import (
	"bytes"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
)

func TestPreparedSampleFinalizesExactlyOnceConcurrently(t *testing.T) {
	t.Parallel()
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	sample, err := NewPreparedSample(envelope, testSampleUsage(t), syntheticRequestReceipt(domain.RequestSample), domain.EmptySamplePresentation(0), func() { calls.Add(1) }, func() {})
	if err != nil {
		t.Fatal(err)
	}
	copy, err := sample.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	if copy.Family() != domain.ProviderOpenAI || copy.Wire() != "responses" || !bytes.Equal(copy.Payload(), envelope.Payload()) {
		t.Fatalf("Envelope() = %#v", copy)
	}
	usage, err := sample.Usage()
	if err != nil || usage.Validate() != nil {
		t.Fatalf("Usage() = %#v, %v", usage, err)
	}
	const count = 32
	var successes atomic.Int32
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if sample.Finalize() == nil {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || calls.Load() != 1 || !sample.Finalized() {
		t.Fatalf("successes=%d calls=%d finalized=%t", successes.Load(), calls.Load(), sample.Finalized())
	}
	if _, err := sample.Envelope(); err == nil {
		t.Fatal("finalized sample unexpectedly returned an envelope")
	}
	if _, err := sample.Usage(); err == nil {
		t.Fatal("finalized sample unexpectedly returned usage")
	}
}

func TestPreparedToolOutputsFinalizesExactlyOnce(t *testing.T) {
	t.Parallel()
	envelope, err := NewNativeCommitEnvelope(domain.ProviderAnthropic, "messages", json.RawMessage(`{"kind":"tool_outputs"}`))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	commit, err := NewPreparedToolOutputs(envelope, func() { calls.Add(1) }, func() {})
	if err != nil {
		t.Fatal(err)
	}
	copy, err := commit.Envelope()
	if err != nil || !bytes.Equal(copy.Payload(), envelope.Payload()) {
		t.Fatalf("tool outputs envelope = %#v, %v", copy, err)
	}
	if err := commit.Finalize(); err != nil {
		t.Fatal(err)
	}
	if err := commit.Finalize(); err == nil || calls.Load() != 1 || !commit.Finalized() {
		t.Fatalf("tool outputs finalizer 不满足恰好一次: calls=%d", calls.Load())
	}
	if _, err := commit.Envelope(); err == nil {
		t.Fatal("已 finalize tool outputs 仍返回 envelope")
	}
	if _, err := NewPreparedToolOutputs(NativeCommitEnvelope{}, func() {}, func() {}); err == nil {
		t.Fatal("零值 envelope 被接受")
	}
	if _, err := NewPreparedToolOutputs(envelope, nil, func() {}); err == nil {
		t.Fatal("nil finalizer 被接受")
	}
	var zero PreparedToolOutputs
	if _, err := zero.Envelope(); err == nil {
		t.Fatal("零值 tool outputs 返回 envelope")
	}
	if err := zero.Finalize(); err == nil {
		t.Fatal("零值 tool outputs 可 finalize")
	}
}

func testSampleUsage(t *testing.T) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(1), domain.UnknownUsageMetric(),
		domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(2),
		domain.NotApplicableUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

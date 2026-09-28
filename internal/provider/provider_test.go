package provider

import (
	"bytes"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
)

func TestNativeCommitEnvelopeDeepCopiesOpaquePayload(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"shape":"text_sample"}`)
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 1, payload)
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = '['
	first := envelope.Payload()
	first[0] = '['
	second := envelope.Payload()
	if !bytes.Equal(second, []byte(`{"shape":"text_sample"}`)) {
		t.Fatalf("payload shares mutable bytes: %s", second)
	}
	clone := envelope.Clone()
	clonePayload := clone.Payload()
	clonePayload[0] = '['
	if !bytes.Equal(envelope.Payload(), []byte(`{"shape":"text_sample"}`)) {
		t.Fatal("Clone() shares mutable payload bytes")
	}
}

func TestNativeCommitEnvelopeRejectsInvalidBoundary(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		family  domain.ProviderFamily
		wire    string
		version int
		payload json.RawMessage
	}{
		{family: "unknown", wire: "responses", version: 1, payload: json.RawMessage(`{}`)},
		{family: domain.ProviderOpenAI, wire: "", version: 1, payload: json.RawMessage(`{}`)},
		{family: domain.ProviderOpenAI, wire: "responses", version: 0, payload: json.RawMessage(`{}`)},
		{family: domain.ProviderOpenAI, wire: "responses", version: 1, payload: json.RawMessage(`{`)},
		{family: domain.ProviderOpenAI, wire: "responses", version: 1, payload: nil},
	}
	for _, fixture := range fixtures {
		if _, err := NewNativeCommitEnvelope(fixture.family, fixture.wire, fixture.version, fixture.payload); err == nil {
			t.Fatalf("NewNativeCommitEnvelope(%#v) unexpectedly succeeded", fixture)
		}
	}
}

func TestPreparedSampleFinalizesExactlyOnceConcurrently(t *testing.T) {
	t.Parallel()
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 1, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	sample, err := NewPreparedSample(envelope, func() { calls.Add(1) })
	if err != nil {
		t.Fatal(err)
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
	copy, err := sample.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	if copy.Family() != domain.ProviderOpenAI || copy.Wire() != "responses" || copy.PayloadVersion() != 1 {
		t.Fatalf("Envelope() = %#v", copy)
	}
}

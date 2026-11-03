package provider

import (
	"easycode/internal/domain"
	"testing"
)

func TestPreparedSampleRequiresAndFreezesReceipt(t *testing.T) {
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewPreparedSample(envelope, testSampleUsage(t), domain.RequestReceipt{}, func() {}, func() {}); err == nil {
		t.Fatal("missing receipt accepted")
	}
	receipt := syntheticRequestReceipt(domain.RequestSample)
	sample, err := NewPreparedSample(envelope, testSampleUsage(t), receipt, func() {}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	receipt.RequestFingerprint = "mutated"
	value, err := sample.Receipt()
	if err != nil || value.RequestFingerprint == "mutated" {
		t.Fatal("receipt shares state", err)
	}
	value.NativeFingerprint = "changed"
	again, err := sample.Receipt()
	if err != nil || again.NativeFingerprint == "changed" {
		t.Fatal("getter shares state")
	}
	if err = sample.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err = sample.Receipt(); err == nil {
		t.Fatal("finalized receipt exposed as pending")
	}
}

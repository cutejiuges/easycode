package provider

import (
	"easycode/internal/domain"
	"sync/atomic"
	"testing"
)

func TestPreparedUserInputsOwnsAssociationAndResolvesOnce(t *testing.T) {
	id := domain.InputID("01900000-0000-7000-8000-000000000001")
	association, err := domain.NewInputAssociation(domain.TurnID(id), domain.InputInitial, 0, []domain.InputID{id})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 2, []byte(`{"kind":"user_inputs"}`))
	if err != nil {
		t.Fatal(err)
	}
	var finalized, discarded atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	inputs, err := NewPreparedUserInputs(association, envelope, func() { finalized.Add(1); close(entered); <-release }, func() { discarded.Add(1) }, testInputPreview(t, domain.ProviderOpenAI))
	if err != nil {
		t.Fatal(err)
	}
	copy, err := inputs.Association()
	if err != nil {
		t.Fatal(err)
	}
	ids := copy.IDs()
	ids[0] = "invalid"
	copy, err = inputs.Association()
	if err != nil || copy.IDs()[0] != id {
		t.Fatal("association changed")
	}
	go func() { done <- inputs.Finalize() }()
	<-entered
	if inputs.Finalized() {
		t.Fatal("finalize reported completion before callback")
	}
	if _, err := inputs.Envelope(); err == nil {
		t.Fatal("resolving commit remained readable")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !inputs.Finalized() || inputs.Discard() == nil || inputs.Finalize() == nil || finalized.Load() != 1 || discarded.Load() != 0 {
		t.Fatal("commit resolved more than once")
	}
}

func TestPreparedUserInputsRejectsIncompleteLifecycle(t *testing.T) {
	id := domain.InputID("01900000-0000-7000-8000-000000000001")
	association, _ := domain.NewInputAssociation(domain.TurnID(id), domain.InputSteering, 1, []domain.InputID{id})
	envelope, _ := NewNativeCommitEnvelope(domain.ProviderAnthropic, "messages", 2, []byte(`{"kind":"user_inputs"}`))
	if _, err := NewPreparedUserInputs(association, envelope, func() {}, nil, testInputPreview(t, domain.ProviderAnthropic)); err == nil {
		t.Fatal("missing discard accepted")
	}
	if _, err := NewPreparedUserInputs(domain.InputAssociation{}, envelope, func() {}, func() {}, testInputPreview(t, domain.ProviderAnthropic)); err == nil {
		t.Fatal("zero association accepted")
	}
	discarded := 0
	inputs, err := NewPreparedUserInputs(association, envelope, func() { t.Fatal("discard called finalize") }, func() { discarded++ }, testInputPreview(t, domain.ProviderAnthropic))
	if err != nil {
		t.Fatal(err)
	}
	if inputs.Discard() != nil || inputs.Finalize() == nil || inputs.Finalized() || discarded != 1 {
		t.Fatal("discard lifecycle failed")
	}
}

func TestRestoreRecordKeepsExplicitRoleAndTurn(t *testing.T) {
	id := domain.InputID("01900000-0000-7000-8000-000000000001")
	association, _ := domain.NewInputAssociation(domain.TurnID(id), domain.InputInitial, 0, []domain.InputID{id})
	envelope, _ := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 2, []byte(`{"kind":"user_inputs"}`))
	record, err := NewInputRestoreRecord(association, envelope)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := record.Association()
	if !ok || a.IDs()[0] != id || record.Role() != domain.NativeCommitUserInputs || record.TurnID() != domain.TurnID(id) {
		t.Fatal("restore association lost")
	}
	sample, err := NewSampleRestoreRecord(domain.TurnID(id), envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sample.Association(); ok {
		t.Fatal("sample acquired an input association")
	}
	if (RestoreRecord{}).Validate() == nil {
		t.Fatal("zero restore record accepted")
	}
	if _, err := NewToolOutputRestoreRecord("", envelope); err == nil {
		t.Fatal("invalid turn accepted")
	}
}

func testInputPreview(t *testing.T, family domain.ProviderFamily) InputPreview {
	t.Helper()
	estimate, err := domain.NewUnknownTokenEstimate("byte_heuristic")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := domain.NewNativeHistoryFootprint(family, 0, estimate)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := NewInputPreview(domain.SemanticHistoryView{Provider: family, Turns: []domain.SemanticTurn{}}, footprint, func(TurnInput) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return preview
}

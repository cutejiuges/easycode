package provider

import (
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
)

func compactionFixture(t *testing.T) (CheckpointEnvelope, domain.ActiveContextView, domain.NativeHistoryFootprint, domain.SampleUsage) {
	t.Helper()
	source, err := NewCompactionSource(domain.ProviderOpenAI, "responses", "model", 2, 2, RootCompactionPredecessor(), strings.Repeat("a", 64), CompactionPromptRevision)
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := domain.NewContextSummary("completed work and next task")
	checkpoint, err := NewCheckpointEnvelope("01900000-0000-7000-8000-000000000001", source, 3, []byte(`{"summary":"native"}`))
	if err != nil {
		t.Fatal(err)
	}
	view, err := domain.NewActiveContextView(domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{}}, summary, 3)
	if err != nil {
		t.Fatal(err)
	}
	estimate, _ := domain.NewEstimatedTokenEstimate("byte_heuristic", 10)
	footprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 3, estimate)
	usage, _ := domain.NewSampleUsage(domain.KnownUsageMetric(20), domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric(), domain.KnownUsageMetric(5), domain.UnknownUsageMetric())
	return checkpoint, view, footprint, usage
}

func TestPreparedCompactionOwnsCandidateAndResolution(t *testing.T) {
	checkpoint, view, footprint, usage := compactionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var finalized, discarded atomic.Int32
	prepared, err := NewPreparedCompaction(checkpoint, view, footprint, usage, testCompactionUsage(t), func() { finalized.Add(1); close(entered); <-release }, func() { discarded.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	copy, err := prepared.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	bytes := copy.Payload()
	bytes[0] = '['
	again, _ := prepared.Checkpoint()
	if string(again.Payload()) != `{"summary":"native"}` {
		t.Fatal("checkpoint alias leaked")
	}
	gotUsage, err := prepared.Usage()
	if err != nil || gotUsage != usage {
		t.Fatal("usage changed")
	}
	result := make(chan error, 1)
	go func() { result <- prepared.Finalize() }()
	<-entered
	if prepared.Finalized() {
		t.Fatal("finalized before callback returned")
	}
	if _, err := prepared.Checkpoint(); err == nil {
		t.Fatal("resolving checkpoint exposed")
	}
	discardedResult := make(chan error, 1)
	go func() { discardedResult <- prepared.Discard() }()
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-discardedResult; err == nil {
		t.Fatal("duplicate resolution accepted")
	}
	if !prepared.Finalized() || finalized.Load() != 1 || discarded.Load() != 0 {
		t.Fatal("resolution not exclusive")
	}
	if _, _, err := prepared.Preview(); err == nil {
		t.Fatal("resolved candidate exposed")
	}
}

func TestPreparedCompactionRejectsSourceMismatchAndZero(t *testing.T) {
	checkpoint, view, footprint, usage := compactionFixture(t)
	if (CompactionSource{}).Validate() == nil {
		t.Fatal("zero source accepted")
	}
	if _, err := (CheckpointEnvelope{}).Clone(); err == nil {
		t.Fatal("zero checkpoint accepted")
	}
	var zero PreparedCompaction
	if zero.Finalize() == nil || zero.Discard() == nil {
		t.Fatal("zero lifecycle accepted")
	}
	summary, _ := domain.NewContextSummary("different summary")
	wrong, _ := domain.NewActiveContextView(view.History(), summary, view.Revision()+1)
	if _, err := NewPreparedCompaction(checkpoint, wrong, footprint, usage, testCompactionUsage(t), func() {}, func() {}); err == nil {
		t.Fatal("revision mismatch accepted")
	}
	if _, err := NewPreparedCompaction(checkpoint, view, footprint, usage, testCompactionUsage(t), nil, func() {}); err == nil {
		t.Fatal("missing lifecycle accepted")
	}
	var discarded int
	prepared, err := NewPreparedCompaction(checkpoint, view, footprint, usage, testCompactionUsage(t), func() { t.Fatal("discard installed checkpoint") }, func() { discarded++ })
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Discard() != nil || discarded != 1 || prepared.Finalized() {
		t.Fatal("discard lifecycle failed")
	}
	if prepared.Finalize() == nil || prepared.Discard() == nil {
		t.Fatal("resolved candidate reused")
	}
	source := checkpoint.Source()
	if _, err := NewCompactionSource(source.Family(), source.Wire(), source.Model(), 2, 2, CompactionPredecessor{}, source.Digest(), source.PromptRevision()); err == nil {
		t.Fatal("implicit root accepted")
	}
	predecessor, _ := AfterCompactionCheckpoint(checkpoint.ID())
	if _, err := NewCompactionSource(source.Family(), source.Wire(), source.Model(), 2, 2, predecessor, source.Digest(), source.PromptRevision()); err == nil {
		t.Fatal("checkpoint without advanced revision accepted")
	}
	if _, err := NewCompactionSource(source.Family(), source.Wire(), source.Model(), 2, 3, RootCompactionPredecessor(), source.Digest(), source.PromptRevision()); err == nil {
		t.Fatal("root with advanced revision accepted")
	}
}

func testCompactionUsage(t *testing.T) NativeCommitEnvelope {
	t.Helper()
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 3, []byte(`{"usage":"fixture"}`))
	if err != nil {
		t.Fatal(err)
	}
	return envelope
}

func TestCheckpointCodecStrictAndIndependent(t *testing.T) {
	checkpoint, _, _, _ := compactionFixture(t)
	encoded, err := checkpoint.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var restored CheckpointEnvelope
	if err := restored.UnmarshalJSON(encoded); err != nil {
		t.Fatal(err)
	}
	if restored.ID() != checkpoint.ID() || restored.Source() != checkpoint.Source() || string(restored.Payload()) != string(checkpoint.Payload()) {
		t.Fatal("checkpoint codec changed identity or bytes")
	}
	for _, bad := range []string{
		strings.Replace(string(encoded), `"payload_version":3`, `"payload_version":null`, 1),
		strings.Replace(string(encoded), `"root":true`, `"root":true,"checkpoint_id":"01900000-0000-7000-8000-000000000009"`, 1),
		strings.Replace(string(encoded), `"root":true`, `"root":false`, 1),
		strings.Replace(string(encoded), `"context_revision":2`, `"context_revision":2,"context_revision":3`, 1),
		strings.Replace(string(encoded), `"prompt_revision":"context-summary-v1"`, `"prompt_revision":"unknown"`, 1),
		strings.Replace(string(encoded), `"committed_revision":2`, `"committed_revision":3`, 1),
		strings.TrimSuffix(string(encoded), "}") + `,"unknown":true}`,
	} {
		before := restored.Source()
		if err := restored.UnmarshalJSON([]byte(bad)); err == nil {
			t.Fatal("invalid checkpoint shape accepted")
		}
		if restored.Source() != before {
			t.Fatal("failed decode partially mutated checkpoint")
		}
	}
}

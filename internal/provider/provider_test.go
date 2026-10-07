package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestTurnInputProjectInstructionsAreOptionalAndImmutable(t *testing.T) {
	t.Parallel()
	plain := TurnInput{Text: "hello"}
	if _, exists, err := plain.ProjectInstructions(); err != nil || exists {
		t.Fatalf("plain project instructions = exists %t, err %v", exists, err)
	}

	document, err := domain.NewProjectInstructionDocument("AGENTS.md", "follow the project rules")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewProjectInstructionsSnapshot([]domain.ProjectInstructionDocument{document}, 32<<10)
	if err != nil {
		t.Fatal(err)
	}
	attached, err := plain.WithProjectInstructions(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	first, exists, err := attached.ProjectInstructions()
	if err != nil || !exists {
		t.Fatalf("attached project instructions = exists %t, err %v", exists, err)
	}
	documents := first.Documents()
	documents[0] = domain.ProjectInstructionDocument{}
	second, exists, err := attached.ProjectInstructions()
	if err != nil || !exists || len(second.Documents()) != 1 || second.Documents()[0].Content() != "follow the project rules" {
		t.Fatalf("project instructions share mutable state: %#v, exists %t, err %v", second, exists, err)
	}
	if attached.Text != "hello" || plain.Text != "hello" {
		t.Fatalf("attaching project instructions changed user text: %#v %#v", attached, plain)
	}
}

func TestTurnInputAcceptsExplicitEmptyProjectInstructions(t *testing.T) {
	t.Parallel()
	snapshot, err := domain.NewEmptyProjectInstructionsSnapshot(32 << 10)
	if err != nil {
		t.Fatal(err)
	}
	input, err := (TurnInput{Text: "hello"}).WithProjectInstructions(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	got, exists, err := input.ProjectInstructions()
	if err != nil || !exists || got.HasDocuments() {
		t.Fatalf("empty project instructions = %#v, exists %t, err %v", got, exists, err)
	}
}

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
	clone, err := envelope.Clone()
	if err != nil {
		t.Fatal(err)
	}
	clonePayload := clone.Payload()
	clonePayload[0] = '['
	if !bytes.Equal(envelope.Payload(), []byte(`{"shape":"text_sample"}`)) {
		t.Fatal("Clone() shares mutable payload bytes")
	}
}

func TestNativeCommitEnvelopeCloneRejectsZeroValue(t *testing.T) {
	t.Parallel()
	if _, err := (NativeCommitEnvelope{}).Clone(); err == nil {
		t.Fatal("zero-value envelope unexpectedly cloned")
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
	sample, err := NewPreparedSample(envelope, testSampleUsage(t), func() { calls.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	copy, err := sample.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	if copy.Family() != domain.ProviderOpenAI || copy.Wire() != "responses" || copy.PayloadVersion() != 1 {
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

func TestPreparedSampleRejectsInvalidConstructionAndZeroValue(t *testing.T) {
	t.Parallel()
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 1, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	usage := testSampleUsage(t)
	if _, err := NewPreparedSample(NativeCommitEnvelope{}, usage, func() {}); err == nil {
		t.Fatal("zero-value envelope unexpectedly prepared")
	}
	if _, err := NewPreparedSample(envelope, domain.SampleUsage{}, func() {}); err == nil {
		t.Fatal("zero-value usage unexpectedly prepared")
	}
	if _, err := NewPreparedSample(envelope, usage, nil); err == nil {
		t.Fatal("nil finalizer unexpectedly accepted")
	}
	var zero PreparedSample
	if _, err := zero.Envelope(); err == nil {
		t.Fatal("zero-value sample unexpectedly returned an envelope")
	}
	if _, err := zero.Usage(); err == nil {
		t.Fatal("zero-value sample unexpectedly returned usage")
	}
	if err := zero.Finalize(); err == nil {
		t.Fatal("zero-value sample unexpectedly finalized")
	}
	var nilSample *PreparedSample
	if _, err := nilSample.Envelope(); err == nil {
		t.Fatal("nil sample unexpectedly returned an envelope")
	}
	if _, err := nilSample.Usage(); err == nil {
		t.Fatal("nil sample unexpectedly returned usage")
	}
	if err := nilSample.Finalize(); err == nil {
		t.Fatal("nil sample unexpectedly finalized")
	}
}

type testNativeItem struct {
	family domain.ProviderFamily
	kind   string
}

func (item testNativeItem) ProviderFamily() domain.ProviderFamily { return item.family }
func (item testNativeItem) ItemKind() string                      { return item.kind }

type pointerTestNativeItem struct{}

func (*pointerTestNativeItem) ProviderFamily() domain.ProviderFamily { return domain.ProviderOpenAI }
func (*pointerTestNativeItem) ItemKind() string                      { return "message" }

type pointerTestError struct{}

func (*pointerTestError) Error() string { return "provider failed" }

func TestStreamEventConstructorsAndAccessors(t *testing.T) {
	t.Parallel()
	semanticPayload := []byte(`{"text":"hello"}`)
	semantic, err := protocol.NewAssistantTextDelta("hello")
	if err != nil {
		t.Fatal(err)
	}
	semantic.Payload = semanticPayload
	sample := newProviderTestSample(t, func() {})
	failure := errors.New("provider failed")

	events := []struct {
		name string
		kind StreamEventKind
		new  func() (StreamEvent, error)
	}{
		{name: "semantic", kind: StreamEventSemantic, new: func() (StreamEvent, error) { return NewSemanticStreamEvent(semantic) }},
		{name: "native", kind: StreamEventNative, new: func() (StreamEvent, error) {
			return NewNativeStreamEvent(testNativeItem{family: domain.ProviderOpenAI, kind: "message"})
		}},
		{name: "completed", kind: StreamEventCompleted, new: func() (StreamEvent, error) { return NewCompletedStreamEvent(sample) }},
		{name: "failed", kind: StreamEventFailed, new: func() (StreamEvent, error) { return NewFailedStreamEvent(failure) }},
		{name: "cancelled", kind: StreamEventCancelled, new: func() (StreamEvent, error) { return NewCancelledStreamEvent(context.Canceled) }},
	}

	for _, test := range events {
		t.Run(test.name, func(t *testing.T) {
			event, buildErr := test.new()
			if buildErr != nil || event.Kind() != test.kind || event.Validate() != nil {
				t.Fatalf("event = %#v, build err = %v, validate err = %v", event, buildErr, event.Validate())
			}
		})
	}

	semanticEvent, _ := NewSemanticStreamEvent(semantic)
	gotSemantic := semanticEvent.Semantic()
	if gotSemantic.Kind != protocol.EventAssistantTextDelta {
		t.Fatalf("Semantic() = %#v", gotSemantic)
	}
	gotSemantic.Payload[0] = '['
	again := semanticEvent.Semantic()
	if string(again.Payload) != `{"text":"hello"}` || string(semantic.Payload) != `{"text":"hello"}` {
		t.Fatal("semantic event shares mutable payload bytes")
	}
	if semanticEvent.NativeItem() != nil {
		t.Fatal("semantic event exposed native payload")
	}
	if semanticEvent.PreparedSample() != nil {
		t.Fatal("semantic event exposed prepared sample")
	}
	if semanticEvent.Error() != nil {
		t.Fatal("semantic event exposed terminal error")
	}

	nativeEvent, _ := NewNativeStreamEvent(testNativeItem{family: domain.ProviderOpenAI, kind: "message"})
	if nativeEvent.NativeItem() == nil {
		t.Fatal("native event did not expose native payload")
	}
	completedEvent, _ := NewCompletedStreamEvent(sample)
	if got := completedEvent.PreparedSample(); got != sample {
		t.Fatalf("PreparedSample() = %#v", got)
	}
	failedEvent, _ := NewFailedStreamEvent(failure)
	if got := failedEvent.Error(); !errors.Is(got, failure) {
		t.Fatalf("Error() = %v", got)
	}
}

func TestStreamEventRejectsInvalidStates(t *testing.T) {
	t.Parallel()
	semantic, err := protocol.NewAssistantTextDelta("hello")
	if err != nil {
		t.Fatal(err)
	}
	sample := newProviderTestSample(t, func() {})
	fixtures := []StreamEvent{
		{},
		{kind: "unknown"},
		{kind: StreamEventSemantic},
		{kind: StreamEventSemantic, semantic: semantic, err: errors.New("conflict")},
		{kind: StreamEventNative},
		{kind: StreamEventNative, native: testNativeItem{family: "unknown", kind: "message"}},
		{kind: StreamEventNative, native: testNativeItem{family: domain.ProviderOpenAI}},
		{kind: StreamEventCompleted},
		{kind: StreamEventCompleted, prepared: sample, err: errors.New("conflict")},
		{kind: StreamEventFailed},
		{kind: StreamEventCancelled},
	}
	for _, event := range fixtures {
		if err := event.Validate(); err == nil {
			t.Fatalf("Validate(%#v) unexpectedly succeeded", event)
		}
	}
	if _, err := NewSemanticStreamEvent(protocol.NewTurnStarted()); err == nil {
		t.Fatal("turn lifecycle event unexpectedly accepted as provider semantic")
	}
	if _, err := NewNativeStreamEvent(testNativeItem{}); err == nil {
		t.Fatal("invalid native item unexpectedly accepted")
	}
	var nilNative *pointerTestNativeItem
	if _, err := NewNativeStreamEvent(nilNative); err == nil {
		t.Fatal("typed nil native item unexpectedly accepted")
	}
	if _, err := NewCompletedStreamEvent(nil); err == nil {
		t.Fatal("nil prepared sample unexpectedly accepted")
	}
	if _, err := NewFailedStreamEvent(nil); err == nil {
		t.Fatal("nil failure unexpectedly accepted")
	}
	if _, err := NewCancelledStreamEvent(nil); err == nil {
		t.Fatal("nil cancellation unexpectedly accepted")
	}
	var nilFailure *pointerTestError
	if _, err := NewFailedStreamEvent(nilFailure); err == nil {
		t.Fatal("typed nil failure unexpectedly accepted")
	}
}

func TestCompletedStreamEventRejectsFinalizedSampleWithoutSideEffects(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	sample := newProviderTestSample(t, func() { calls.Add(1) })
	if _, err := NewCompletedStreamEvent(sample); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || sample.Finalized() {
		t.Fatal("completed event validation finalized sample")
	}
	if err := sample.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCompletedStreamEvent(sample); err == nil {
		t.Fatal("finalized sample unexpectedly accepted")
	}
	if calls.Load() != 1 {
		t.Fatalf("finalizer calls = %d", calls.Load())
	}
}

func newProviderTestSample(t *testing.T, finalize func()) *PreparedSample {
	t.Helper()
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 1, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	sample, err := NewPreparedSample(envelope, testSampleUsage(t), finalize)
	if err != nil {
		t.Fatal(err)
	}
	return sample
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

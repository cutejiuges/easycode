package context

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestStablePrefixFingerprintIgnoresVolatileTail(t *testing.T) {
	stable := mustSegment(t, "base", StabilityStable, "v1", map[string]any{"z": 1, "a": 2})
	firstTail := mustSegment(t, "world", StabilityVolatile, "turn-1", map[string]any{"cwd": "/one"})
	secondTail := mustSegment(t, "world", StabilityVolatile, "turn-2", map[string]any{"cwd": "/two"})

	firstPlan, err := NewPlan(stable, firstTail)
	if err != nil {
		t.Fatalf("create first plan: %v", err)
	}
	firstFingerprint, err := firstPlan.StablePrefixFingerprint()
	if err != nil {
		t.Fatalf("fingerprint first plan: %v", err)
	}
	secondPlan, err := NewPlan(stable, secondTail)
	if err != nil {
		t.Fatalf("create second plan: %v", err)
	}
	secondFingerprint, err := secondPlan.StablePrefixFingerprint()
	if err != nil {
		t.Fatalf("fingerprint second plan: %v", err)
	}

	if firstFingerprint != secondFingerprint {
		t.Fatalf("volatile tail changed stable prefix: %s != %s", firstFingerprint, secondFingerprint)
	}
}

func TestSegmentAndPlanAreImmutableSnapshots(t *testing.T) {
	raw := []byte(`{"a":2,"z":1}`)
	canonical, err := codec.ParseCanonical(raw, MaxSegmentBytes)
	if err != nil {
		t.Fatalf("parse canonical JSON: %v", err)
	}
	segment, err := NewSegment("base", StabilityStable, "v1", canonical)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}
	input := []Segment{segment}
	plan, err := NewPlan(input...)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	wantFingerprint, err := plan.StablePrefixFingerprint()
	if err != nil {
		t.Fatalf("fingerprint plan: %v", err)
	}

	raw[2] = 'x'
	canonicalBytes := canonical.Bytes()
	canonicalBytes[2] = 'y'
	input[0] = Segment{}
	gotSegments := plan.Segments()
	getterBytes := gotSegments[0].CanonicalJSON()
	getterBytes[0] = '['
	gotSegments[0] = Segment{}

	actual := plan.Segments()
	if len(actual) != 1 || actual[0].ID() != "base" {
		t.Fatalf("plan changed through caller-owned data: segments=%#v", actual)
	}
	if got := string(actual[0].CanonicalJSON()); got != `{"a":2,"z":1}` {
		t.Fatalf("segment bytes changed: %s", got)
	}
	output := actual[0].CanonicalJSON()
	output[0] = '['
	if got := string(plan.Segments()[0].CanonicalJSON()); got != `{"a":2,"z":1}` {
		t.Fatalf("segment getter leaked bytes: %s", got)
	}
	gotFingerprint, err := plan.StablePrefixFingerprint()
	if err != nil {
		t.Fatalf("fingerprint mutated plan: %v", err)
	}
	if gotFingerprint != wantFingerprint {
		t.Fatalf("plan fingerprint changed: %s != %s", gotFingerprint, wantFingerprint)
	}
}

func TestPlanRejectsInvalidMetadataOrderingAndFingerprint(t *testing.T) {
	stable := mustSegment(t, "tools", StabilityStable, "v1", struct{}{})
	volatile := mustSegment(t, "world", StabilityVolatile, "v1", struct{}{})

	tests := []struct {
		name     string
		segments []Segment
	}{
		{name: "duplicate ID", segments: []Segment{stable, stable}},
		{name: "stable after volatile", segments: []Segment{volatile, stable}},
		{name: "fingerprint mismatch", segments: []Segment{withFingerprint(stable, "wrong")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPlan(test.segments...); err == nil {
				t.Fatal("expected invalid cache plan")
			}
		})
	}
}

func TestNewSegmentRejectsInvalidMetadata(t *testing.T) {
	canonical, err := codec.MarshalCanonical(struct{}{}, MaxSegmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		id        string
		stability Stability
		revision  string
		content   codec.CanonicalJSON
	}{
		{name: "empty ID", stability: StabilityStable, revision: "v1", content: canonical},
		{name: "unknown stability", id: "base", stability: "future", revision: "v1", content: canonical},
		{name: "empty revision", id: "base", stability: StabilityStable, content: canonical},
		{name: "empty content", id: "base", stability: StabilityStable, revision: "v1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSegment(test.id, test.stability, test.revision, test.content); err == nil {
				t.Fatal("expected invalid cache segment")
			}
		})
	}

	oversized := strings.Repeat("x", MaxSegmentBytes)
	large, err := codec.MarshalCanonical(oversized, codec.DefaultMaxCanonicalJSONBytes)
	if err != nil {
		t.Fatalf("marshal oversized value: %v", err)
	}
	if _, err := NewSegment("base", StabilityStable, "v1", large); err == nil {
		t.Fatal("expected oversized cache segment")
	}
}

func TestSourceRevisionInvalidatesStablePrefixFingerprint(t *testing.T) {
	first := mustSegment(t, "base", StabilityStable, "v1", map[string]int{"value": 1})
	second := mustSegment(t, "base", StabilityStable, "v2", map[string]int{"value": 1})
	firstPlan, err := NewPlan(first)
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := NewPlan(second)
	if err != nil {
		t.Fatal(err)
	}
	firstFingerprint, _ := firstPlan.StablePrefixFingerprint()
	secondFingerprint, _ := secondPlan.StablePrefixFingerprint()
	if firstFingerprint == secondFingerprint {
		t.Fatalf("source revision did not invalidate fingerprint: %s", firstFingerprint)
	}
	if !bytes.Equal(first.CanonicalJSON(), second.CanonicalJSON()) {
		t.Fatal("test setup changed content bytes")
	}
}

func mustSegment(t *testing.T, id string, stability Stability, revision string, value any) Segment {
	t.Helper()
	canonical, err := codec.MarshalCanonical(value, MaxSegmentBytes)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}
	segment, err := NewSegment(id, stability, revision, canonical)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}
	return segment
}

func withFingerprint(segment Segment, fingerprint string) Segment {
	segment.fingerprint = fingerprint
	return segment
}

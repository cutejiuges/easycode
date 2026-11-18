package context

import (
	"bytes"
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

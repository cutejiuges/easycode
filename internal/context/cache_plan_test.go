package context

import "testing"

func TestStablePrefixFingerprintIgnoresVolatileTail(t *testing.T) {
	stable, err := NewSegment("base", StabilityStable, "v1", map[string]any{"z": 1, "a": 2})
	if err != nil {
		t.Fatalf("create stable segment: %v", err)
	}
	firstTail, err := NewSegment("world", StabilityVolatile, "turn-1", map[string]any{"cwd": "/one"})
	if err != nil {
		t.Fatalf("create first tail: %v", err)
	}
	secondTail, err := NewSegment("world", StabilityVolatile, "turn-2", map[string]any{"cwd": "/two"})
	if err != nil {
		t.Fatalf("create second tail: %v", err)
	}

	firstFingerprint, err := NewPlan(stable, firstTail).StablePrefixFingerprint()
	if err != nil {
		t.Fatalf("fingerprint first plan: %v", err)
	}
	secondFingerprint, err := NewPlan(stable, secondTail).StablePrefixFingerprint()
	if err != nil {
		t.Fatalf("fingerprint second plan: %v", err)
	}

	if firstFingerprint != secondFingerprint {
		t.Fatalf("volatile tail changed stable prefix: %s != %s", firstFingerprint, secondFingerprint)
	}
}

func TestPlanRejectsStableSegmentAfterVolatileSegment(t *testing.T) {
	volatile, err := NewSegment("world", StabilityVolatile, "v1", struct{}{})
	if err != nil {
		t.Fatalf("create volatile segment: %v", err)
	}
	stable, err := NewSegment("tools", StabilityStable, "v1", struct{}{})
	if err != nil {
		t.Fatalf("create stable segment: %v", err)
	}

	if err := NewPlan(volatile, stable).Validate(); err == nil {
		t.Fatal("expected invalid segment order")
	}
}

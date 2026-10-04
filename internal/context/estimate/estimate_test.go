package estimate

import (
	"math"
	"testing"
)

func TestByteCountRoundsUpDeterministically(t *testing.T) {
	t.Parallel()
	tests := []struct {
		bytes uint64
		want  uint64
	}{{0, 0}, {1, 1}, {3, 1}, {4, 1}, {5, 2}, {8, 2}}
	for _, test := range tests {
		if got := ByteCount(test.bytes); got != test.want {
			t.Fatalf("ByteCount(%d) = %d, want %d", test.bytes, got, test.want)
		}
	}
	if got := ByteCount(math.MaxUint64); got != math.MaxUint64/4+1 {
		t.Fatalf("ByteCount(max) = %d", got)
	}
}

func TestStringCountsUTF8Bytes(t *testing.T) {
	t.Parallel()
	estimate := String("你a")
	if tokens, ok := estimate.Tokens(); !ok || tokens != 1 || estimate.Method() != MethodByteHeuristicV1 {
		t.Fatalf("estimate = method %q tokens %d ok %t", estimate.Method(), tokens, ok)
	}
}

func TestStructuredByteCountIncludesFramingAndSaturates(t *testing.T) {
	t.Parallel()
	if got := StructuredByteCount(4, 4); got != 2 {
		t.Fatalf("structured count = %d", got)
	}
	if got := StructuredByteCount(math.MaxUint64, 1); got != math.MaxUint64/4+1 {
		t.Fatalf("saturated structured count = %d", got)
	}
}

func TestSaturatingAddNeverWraps(t *testing.T) {
	t.Parallel()
	if got := SaturatingAdd(math.MaxUint64, 1); got != math.MaxUint64 {
		t.Fatalf("overflow = %d", got)
	}
	if got := SaturatingAdd(1, 2, 3); got != 6 {
		t.Fatalf("sum = %d", got)
	}
}

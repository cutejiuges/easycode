package provider

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"testing"
)

func TestRequestBaselineUsesExactImmutableNativePrefix(t *testing.T) {
	part := func(text string) codec.CanonicalJSON {
		v, err := codec.ParseCanonical([]byte(text), MaxNativeCommitBytes)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	input := []codec.CanonicalJSON{part(`{"text":"question"}`)}
	output := []codec.CanonicalJSON{part(`{"encrypted_content":"opaque-a","phase":"final"}`)}
	properties := part(`{"model":"fixture"}`)
	baseline, err := NewRequestBaseline(input, output, properties)
	if err != nil {
		t.Fatal(err)
	}
	current := append(append([]codec.CanonicalJSON(nil), input...), output...)
	current = append(current, part(`{"text":"next"}`))
	input[0] = part(`{"text":"caller-mutated"}`)
	output[0] = part(`{"encrypted_content":"caller-mutated"}`)
	offset, relation, reason := baseline.Compare(current, properties)
	if offset != 2 || relation != domain.NativeAppend || reason != domain.ResetNone {
		t.Fatal("valid prefix lost", offset, relation, reason)
	}
	current[1] = part(`{"encrypted_content":"opaque-b","phase":"final"}`)
	if _, r, reason := baseline.Compare(current, properties); r != domain.NativeReplaced || reason != domain.ResetPrefix {
		t.Fatal("opaque difference ignored")
	}
	if _, _, reason := baseline.Compare(current, part(`{"model":"other"}`)); reason != domain.ResetProperties {
		t.Fatal("property change ignored")
	}
	if _, _, reason := baseline.Compare(current[:1], properties); reason != domain.ResetPrefix {
		t.Fatal("shortened context accepted")
	}
	if _, r, reason := (RequestBaseline{}).Compare(current, properties); r != domain.NativeFirst || reason != domain.ResetFirst {
		t.Fatal("missing baseline misclassified")
	}
	if _, err = NewRequestBaseline(nil, nil, codec.CanonicalJSON{}); err == nil {
		t.Fatal("zero properties accepted")
	}
}

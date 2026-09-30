package codec

import (
	"bytes"
	"testing"
)

func TestMarshalStableSortsMapKeys(t *testing.T) {
	first := map[string]any{"z": 1, "a": 2}
	second := map[string]any{"a": 2, "z": 1}

	firstJSON, err := MarshalStable(first)
	if err != nil {
		t.Fatalf("marshal first value: %v", err)
	}
	secondJSON, err := MarshalStable(second)
	if err != nil {
		t.Fatalf("marshal second value: %v", err)
	}

	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("stable JSON differs: %s != %s", firstJSON, secondJSON)
	}
}

func TestUnmarshalStrictRejectsUnknownFields(t *testing.T) {
	var target struct {
		Value string `json:"value"`
	}
	if err := UnmarshalStrict([]byte(`{"value":"ok","extra":true}`), &target); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestCanonicalJSONRejectsInvalidNonCanonicalAndOversizedBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		maxBytes int
	}{
		{name: "empty", input: nil, maxBytes: 16},
		{name: "invalid", input: []byte(`{"a":`), maxBytes: 16},
		{name: "trailing value", input: []byte(`{} {}`), maxBytes: 16},
		{name: "whitespace", input: []byte(`{ "a":1}`), maxBytes: 16},
		{name: "unsorted keys", input: []byte(`{"z":1,"a":2}`), maxBytes: 32},
		{name: "oversized", input: []byte(`{"value":1}`), maxBytes: 4},
		{name: "invalid limit", input: []byte(`{}`), maxBytes: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseCanonical(test.input, test.maxBytes); err == nil {
				t.Fatal("expected canonical JSON validation error")
			}
		})
	}
}

func TestCanonicalJSONIsStableAndImmutable(t *testing.T) {
	first, err := MarshalCanonical(map[string]int{"z": 1, "a": 2}, 64)
	if err != nil {
		t.Fatalf("marshal first canonical JSON: %v", err)
	}
	second, err := MarshalCanonical(map[string]int{"a": 2, "z": 1}, 64)
	if err != nil {
		t.Fatalf("marshal second canonical JSON: %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("equivalent values differ: %s != %s", first.Bytes(), second.Bytes())
	}

	input := []byte(`{"a":2,"z":1}`)
	parsed, err := ParseCanonical(input, 64)
	if err != nil {
		t.Fatalf("parse canonical JSON: %v", err)
	}
	input[2] = 'x'
	output := parsed.Bytes()
	output[2] = 'y'
	if got := string(parsed.Bytes()); got != `{"a":2,"z":1}` {
		t.Fatalf("canonical JSON changed through caller-owned bytes: %s", got)
	}

	clone := parsed.Clone()
	cloneBytes := clone.Bytes()
	cloneBytes[2] = 'q'
	if !bytes.Equal(clone.Bytes(), parsed.Bytes()) {
		t.Fatalf("clone differs: %s != %s", clone.Bytes(), parsed.Bytes())
	}
	if err := parsed.Validate(64); err != nil {
		t.Fatalf("validate canonical JSON: %v", err)
	}
}

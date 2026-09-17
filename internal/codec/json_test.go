package codec

import "testing"

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

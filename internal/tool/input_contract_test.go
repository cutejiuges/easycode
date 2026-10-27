package tool

import (
	"strings"
	"testing"
)

func TestToolInputsRejectAmbiguity(t *testing.T) {
	tests := []struct {
		name, valid, optional string
		decode                func([]byte) error
	}{
		{"Read", `{"file_path":"a"}`, "offset", func(b []byte) error { _, e := DecodeReadInput(b); return e }},
		{"Glob", `{"pattern":"*"}`, "limit", func(b []byte) error { _, e := DecodeGlobInput(b); return e }},
		{"Grep", `{"pattern":"x"}`, "case_insensitive", func(b []byte) error { _, e := DecodeGrepInput(b); return e }},
		{"Patch", `{"file_path":"a","old_string":"x","new_string":"y"}`, "replace_all", func(b []byte) error { _, e := DecodeAnthropicEditInput(b); return e }},
		{"Write", `{"file_path":"a","content":"x"}`, "content", func(b []byte) error { _, e := DecodeWriteInput(b); return e }},
		{"Exec", `{"command":"echo ok"}`, "timeout_ms", func(b []byte) error { _, e := DecodeExecInput(b); return e }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.decode([]byte(tt.valid)); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{tt.valid + `{}`, strings.TrimSuffix(tt.valid, "}") + `,"` + tt.optional + `":null}`, strings.Replace(tt.valid, `"`, `"UNKNOWN`, 1), strings.TrimSuffix(tt.valid, "}") + `,"extra":"\ud800"}`} {
				if err := tt.decode([]byte(bad)); err == nil {
					t.Fatalf("accepted %s", bad)
				}
			}
		})
	}
	read, err := DecodeReadInput([]byte(`{"file_path":"a"}`))
	if err != nil || read.Offset() != 1 || read.Limit() != 2000 {
		t.Fatalf("Read defaults: %#v %v", read, err)
	}
}

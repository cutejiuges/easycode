package tool

import (
	"strings"
	"testing"
)

func TestGlobInputStrictDecodeDefaultsAndBounds(t *testing.T) {
	t.Parallel()
	input, err := DecodeGlobInput([]byte(`{"pattern":"internal/**/*.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if input.Pattern() != "internal/**/*.go" || input.Path() != "" || input.Limit() != 100 {
		t.Fatalf("Glob 默认值错误: %#v", input)
	}
	valid, err := NewGlobInput("*.go", "internal/tool", 1000)
	if err != nil || valid.Path() != "internal/tool" {
		t.Fatalf("构造合法 Glob: %#v %v", valid, err)
	}
	tests := [][]byte{
		[]byte(`{"pattern":""}`),
		[]byte(`{"pattern":"*.go","path":"/tmp"}`),
		[]byte(`{"pattern":"*.go","path":"../tmp"}`),
		[]byte(`{"pattern":"["}`),
		[]byte(`{"pattern":"*.go","limit":0}`),
		[]byte(`{"pattern":"*.go","limit":1001}`),
		[]byte(`{"pattern":"*.go","extra":true}`),
		[]byte(`{"pattern":"*.go","pattern":"*.md"}`),
		[]byte(`{"pattern":"*.go"}{}`),
		{'{', '"', 'p', 'a', 't', 't', 'e', 'r', 'n', '"', ':', '"', 0xff, '"', '}'},
	}
	for index, data := range tests {
		if _, err := DecodeGlobInput(data); err == nil {
			t.Fatalf("非法 Glob %d 未被拒绝", index)
		}
	}
	if _, err := NewGlobInput(strings.Repeat("a", maxSearchPatternBytes+1), "", 1); err == nil {
		t.Fatal("超长 Glob pattern 未被拒绝")
	}
	if _, err := NewGlobInput(strings.Repeat("a/", maxGlobSegments)+"a", "", 1); err == nil {
		t.Fatal("过多 Glob segment 未被拒绝")
	}
}

func TestGrepInputStrictDecodeDefaultsAndBounds(t *testing.T) {
	t.Parallel()
	input, err := DecodeGrepInput([]byte(`{"pattern":"TODO"}`))
	if err != nil {
		t.Fatal(err)
	}
	if input.OutputMode() != GrepOutputContent || input.Limit() != 250 || input.CaseInsensitive() || input.BeforeContext() != 0 || input.AfterContext() != 0 {
		t.Fatalf("Grep 默认值错误: %#v", input)
	}
	valid, err := DecodeGrepInput([]byte(`{"pattern":"todo","path":"internal","glob":"**/*.go","output_mode":"count","case_insensitive":true,"before_context":20,"after_context":20,"limit":1000}`))
	if err != nil || valid.OutputMode() != GrepOutputCount || !valid.CaseInsensitive() {
		t.Fatalf("解码合法 Grep: %#v %v", valid, err)
	}
	tests := [][]byte{
		[]byte(`{"pattern":""}`),
		[]byte(`{"pattern":"("}`),
		[]byte(`{"pattern":"x","path":"/tmp"}`),
		[]byte(`{"pattern":"x","path":"a/../b"}`),
		[]byte(`{"pattern":"x","glob":"["}`),
		[]byte(`{"pattern":"x","output_mode":"future"}`),
		[]byte(`{"pattern":"x","before_context":-1}`),
		[]byte(`{"pattern":"x","after_context":21}`),
		[]byte(`{"pattern":"x","limit":0}`),
		[]byte(`{"pattern":"x","limit":1001}`),
		[]byte(`{"pattern":"x","unknown":1}`),
		[]byte(`{"pattern":"x"}[]`),
		{'{', '"', 'p', 'a', 't', 't', 'e', 'r', 'n', '"', ':', '"', 0xff, '"', '}'},
	}
	for index, data := range tests {
		if _, err := DecodeGrepInput(data); err == nil {
			t.Fatalf("非法 Grep %d 未被拒绝", index)
		}
	}
	if _, err := NewGrepInput(strings.Repeat("x", maxSearchPatternBytes+1), "", "", "", false, 0, 0, 1); err == nil {
		t.Fatal("超长 Grep pattern 未被拒绝")
	}
}

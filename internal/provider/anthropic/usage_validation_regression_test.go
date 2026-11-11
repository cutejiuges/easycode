package anthropic

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"
)

func TestThinkingUsageRejectsAmbiguousOrNonIntegerValues(t *testing.T) {
	for _, token := range []string{"null", " null ", "\nnull\t", "-1", "1.5", "1.0", "1e1", `"30"`, "true", "false", "{}", "[]", "18446744073709551616"} {
		t.Run(strconv.Quote(token), func(t *testing.T) {
			value, err := decodeThinkingUsage(json.RawMessage(`{"thinking_tokens":` + token + `}`))
			if err == nil {
				t.Fatalf("invalid thinking count accepted as %+v", value)
			}
		})
	}
}

func TestThinkingUsageDuplicateDetailsCannotOverrideInvalidFacts(t *testing.T) {
	for _, usage := range []string{
		`{"output_tokens":40,"output_tokens_details":{"thinking_tokens":null},"output_tokens_details":{"thinking_tokens":30}}`,
		`{"output_tokens":40,"output_tokens_details":{"thinking_tokens":30},"output_tokens_details":{"thinking_tokens":0}}`,
		`{"output_tokens":20,"output_tokens":40,"output_tokens_details":{"thinking_tokens":30}}`,
	} {
		t.Run(usage, func(t *testing.T) {
			r := newStreamReducer(testAnthropicToolCatalog(t))
			if _, err := reduceAnthropicTestEvent(r, `{"type":"message_start","message":{"id":"usage-regression","model":"model"}}`); err != nil {
				t.Fatal(err)
			}
			if _, err := reduceAnthropicTestEvent(r, `{"type":"message_delta","usage":`+usage+`}`); err == nil {
				t.Fatal("ambiguous usage accepted")
			}
			if r.metadata.Usage.ThinkingTokens.Known || r.metadata.Usage.OutputTokens.Known {
				t.Fatal("invalid usage changed staging facts")
			}
		})
	}
}

func TestThinkingUsageLatestObservationPreservesIndependentZero(t *testing.T) {
	for _, tc := range []struct {
		name    string
		updates []string
		value   uint64
		known   bool
	}{
		{"unreported", []string{`{}`, `{"future":{"valid":true}}`}, 0, false},
		{"missing-preserves-positive", []string{`{"thinking_tokens":30}`, `{}`}, 30, true},
		{"zero-overrides-positive", []string{`{"thinking_tokens":30}`, `{"thinking_tokens":0}`, `{}`}, 0, true},
		{"positive-overrides-zero", []string{`{"thinking_tokens":0}`, `{"thinking_tokens":30,"future":{"valid":true}}`}, 30, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newStreamReducer(testAnthropicToolCatalog(t))
			for _, event := range []string{
				`{"type":"message_start","message":{"id":"observations","model":"model"},"future":{"valid":true}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
				`{"type":"content_block_stop","index":0}`,
			} {
				if _, err := reduceAnthropicTestEvent(r, event); err != nil {
					t.Fatal(err)
				}
			}
			for _, details := range tc.updates {
				if _, err := reduceAnthropicTestEvent(r, `{"type":"message_delta","usage":{"output_tokens":40,"output_tokens_details":`+details+`},"future":{"valid":true}}`); err != nil {
					t.Fatal("valid provider extension rejected", err)
				}
			}
			if _, err := reduceAnthropicTestEvent(r, `{"type":"message_stop"}`); err != nil {
				t.Fatal(err)
			}
			usage, err := r.sampleUsage()
			if err != nil {
				t.Fatal(err)
			}
			if value, known := usage.ReasoningOutput().Value(); value != tc.value || known != tc.known {
				t.Fatal("thinking observations changed state or applied input placeholder-zero rules")
			}
		})
	}
}

// FuzzThinkingUsageStrictInteger 使用标准整数解析作独立判定，避免 null、浮点或溢出被记为已知零。
func FuzzThinkingUsageStrictInteger(f *testing.F) {
	for _, token := range []string{"0", "30", " 30 ", "18446744073709551615", "18446744073709551616", "null", " null ", "1.0", "1e1", "-1", `"0"`} {
		f.Add(token)
	}
	f.Fuzz(func(t *testing.T, token string) {
		// 参数域为单个 JSON 值，逗号注入后的多成员对象属于另一类帧测试。
		if !json.Valid([]byte(token)) {
			return
		}
		raw := []byte(`{"thinking_tokens":` + token + `}`)
		trimmed := bytes.TrimSpace([]byte(token))
		valid := json.Valid(raw) && len(trimmed) > 0
		for _, b := range trimmed {
			valid = valid && b >= '0' && b <= '9'
		}
		want, parseErr := strconv.ParseUint(string(trimmed), 10, 64)
		valid = valid && parseErr == nil
		got, err := decodeThinkingUsage(raw)
		if (err == nil) != valid {
			t.Fatalf("thinking count %q: valid=%t got=%+v err=%v", token, valid, got, err)
		}
		if valid && (!got.Known || got.Value != want) {
			t.Fatalf("thinking count %q changed: got=%+v want=%d", token, got, want)
		}
	})
}

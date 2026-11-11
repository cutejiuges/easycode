package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

func TestThinkingUsageFinalObservations(t *testing.T) {
	for _, tc := range []struct {
		details      string
		valid, known bool
		value        uint64
	}{
		{``, true, false, 0}, {`,"output_tokens_details":{}`, true, false, 0},
		{`,"output_tokens_details":{"thinking_tokens":30}`, true, true, 30},
		{`,"output_tokens_details":{"thinking_tokens":0}`, true, true, 0},
		{`,"output_tokens_details":{"thinking_tokens":41}`, false, false, 0},
		{`,"output_tokens_details":{"thinking_tokens":-1}`, false, false, 0},
		{`,"output_tokens_details":{"thinking_tokens":1.5}`, false, false, 0},
		{`,"output_tokens_details":{"thinking_tokens":18446744073709551616}`, false, false, 0},
		{`,"output_tokens_details":{"thinking_tokens":null}`, false, false, 0},
		{`,"output_tokens_details":null`, false, false, 0},
		{`,"output_tokens_details":"bad"`, false, false, 0},
		{`,"output_tokens_details":{"thinking_tokens":1,"thinking_tokens":2}`, false, false, 0},
	} {
		t.Run(tc.details, func(t *testing.T) {
			r := newStreamReducer(testAnthropicToolCatalog(t))
			var failure error
			for _, event := range []string{
				`{"type":"message_start","message":{"id":"msg","model":"model","usage":{"input_tokens":12,"cache_creation_input_tokens":3,"cache_read_input_tokens":7}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":40` + tc.details + `}}`,
				`{"type":"message_delta","usage":{}}`,
				`{"type":"message_stop"}`,
			} {
				if _, failure = r.reduce(*anthropicEvent(event).Event); failure != nil {
					break
				}
			}
			if (failure == nil) != tc.valid {
				t.Fatal("validity", tc.valid, failure)
			}
			if !tc.valid {
				return
			}
			u, err := r.sampleUsage()
			if err != nil {
				t.Fatal(err)
			}
			v, k := u.ReasoningOutput().Value()
			if k != tc.known || v != tc.value {
				t.Fatal(u)
			}
			if r.metadata.Usage.InputTokens.Value != 12 || r.metadata.Usage.CacheCreationInputTokens.Value != 3 || r.metadata.Usage.CacheReadInputTokens.Value != 7 {
				t.Fatal("placeholder zero erased start")
			}
			if value, _ := u.Output().Value(); value != 40 {
				t.Fatal("thinking double counted")
			}
		})
	}
}

func TestThinkingUsageCurrentCodecAndCrossValidation(t *testing.T) {
	for _, state := range []optionalUint{{}, {Known: true}, {Known: true, Value: 30}} {
		raw := rawUsage{ThinkingTokens: state}
		usage, err := raw.normalized()
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := codec.MarshalStable(encodeMetadata(messageMetadata{Usage: raw}).Usage)
		if err != nil {
			t.Fatal(err)
		}
		envelope, _ := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, encoded)
		if err := validateCompactionUsage(envelope, usage); err != nil {
			t.Fatal(err)
		}
		if usage.Output().State() != domain.UsageMetricUnknown {
			t.Fatal("invented total")
		}
		for _, bad := range []string{``, `null`, `{"known":true}`, `{"known":false,"value":0}`, `{"known":true,"value":null}`, `{"known":true,"value":-1}`, `{"known":false,"extra":1}`, `{"known":false,"known":false}`} {
			members, _ := codec.ObjectMembers(encoded)
			if bad == "" {
				delete(members, "thinking_tokens")
			} else {
				members["thinking_tokens"] = json.RawMessage(bad)
			}
			payload, _ := codec.MarshalStable(members)
			invalid, _ := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, payload)
			if validateCompactionUsage(invalid, usage) == nil {
				t.Fatal("invalid current state accepted", bad)
			}
		}
		if state.Known && state.Value == 30 {
			for _, wrong := range []domain.UsageMetric{domain.KnownUsageMetric(0), domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric()} {
				mismatch, _ := domain.NewSampleUsage(usage.InputUncached(), usage.CacheRead(), usage.CacheWrite(), usage.Output(), wrong)
				if validateCompactionUsage(envelope, mismatch) == nil {
					t.Fatal("mismatched normalized usage accepted")
				}
			}
		}
	}
	entry := compactionTestGeneration("summary", "opaque")
	entry.Metadata.Usage = rawUsage{OutputTokens: optionalUint{Known: true, Value: 40}, ThinkingTokens: optionalUint{Known: true, Value: 30}}
	envelope, err := encodeNativeCommit(entry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeCommit(envelope)
	if err != nil || decoded.Metadata.Usage != entry.Metadata.Usage {
		t.Fatal(decoded, err)
	}
	old := bytes.Replace(envelope.Payload(), []byte(`,"thinking_tokens":{"known":true,"value":30}`), nil, 1)
	if bytes.Equal(old, envelope.Payload()) {
		t.Fatal("old shape fixture was not changed")
	}
	invalid, _ := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, messagesWire, old)
	if _, err := decodeNativeCommit(invalid); err == nil {
		t.Fatal("old four field shape accepted")
	}
	if strings.Contains(string(old), "thinking_tokens") {
		t.Fatal("old fixture still contains thinking")
	}
}

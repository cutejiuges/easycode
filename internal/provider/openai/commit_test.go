package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

func TestNativeCommitCanonicalGolden(t *testing.T) {
	t.Parallel()
	envelope, err := encodeNativeCommit(nativeHistoryEntry{
		Kind: nativeHistorySample,
		Outputs: []NativeItem{{
			Type: "message", ID: "msg-1", Role: "assistant", Phase: "final",
			Content: []ContentPart{{Type: "output_text", Text: "world"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"kind":"sample","outputs":[{"type":"message","id":"msg-1","role":"assistant","phase":"final","content":[{"type":"output_text","text":"world"}]}],"usage":{"input_tokens":{"known":false},"cached_input_tokens":{"known":false},"cache_write_tokens":{"known":false},"output_tokens":{"known":false},"reasoning_output_tokens":{"known":false}}}`
	if got := string(envelope.Payload()); got != want {
		t.Fatalf("OpenAI native commit golden changed:\n%s", got)
	}
}

func TestNativeCommitRejectsIncompatibleAndCorruptPayloads(t *testing.T) {
	t.Parallel()
	validPayload := json.RawMessage(`{"kind":"sample","outputs":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"world"}]}],"usage":{"input_tokens":{"known":false},"cached_input_tokens":{"known":false},"cache_write_tokens":{"known":false},"output_tokens":{"known":false},"reasoning_output_tokens":{"known":false}}}`)
	fixtures := []struct {
		name    string
		family  domain.ProviderFamily
		wire    string
		payload json.RawMessage
	}{
		{name: "family", family: domain.ProviderAnthropic, wire: responsesWire, payload: validPayload},
		{name: "wire", family: domain.ProviderOpenAI, wire: "chat_completions", payload: validPayload},
		{name: "unexpected version field", family: domain.ProviderOpenAI, wire: responsesWire, payload: append([]byte(`{"payload_version":3,`), validPayload[1:]...)},
		{name: "kind", family: domain.ProviderOpenAI, wire: responsesWire, payload: json.RawMessage(`{"kind":"future","outputs":[{"type":"message","role":"assistant"}]}`)},
		{name: "missing outputs", family: domain.ProviderOpenAI, wire: responsesWire, payload: json.RawMessage(`{"kind":"sample","outputs":[],"usage":{"input_tokens":{"known":false},"cached_input_tokens":{"known":false},"cache_write_tokens":{"known":false},"output_tokens":{"known":false},"reasoning_output_tokens":{"known":false}}}`)},
		{name: "unknown outer", family: domain.ProviderOpenAI, wire: responsesWire, payload: json.RawMessage(`{"kind":"sample","outputs":[{"type":"message","role":"assistant"}],"future":true}`)},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			envelope, err := provider.NewNativeCommitEnvelope(fixture.family, fixture.wire, fixture.payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeNativeCommit(envelope); err == nil || strings.Contains(err.Error(), "hello") {
				t.Fatalf("decodeNativeCommit() error = %v", err)
			}
		})
	}
}

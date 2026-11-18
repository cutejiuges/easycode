package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

func TestNativeCommitCanonicalGolden(t *testing.T) {
	t.Parallel()
	envelope, err := encodeNativeCommit(nativeHistoryEntry{
		Kind:      nativeHistorySample,
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "world"}}},
		Metadata: messageMetadata{
			ID: "msg-1", Model: "claude-test", StopReason: optionalString{Known: true, Value: "end_turn"},
			Usage: rawUsage{InputTokens: optionalUint{Known: true, Value: 3}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"kind":"sample","assistant":{"role":"assistant","content":[{"type":"text","text":"world"}]},"metadata":{"id":"msg-1","model":"claude-test","stop_reason":{"known":true,"value":"end_turn"},"usage":{"input_tokens":{"known":true,"value":3},"cache_creation_input_tokens":{"known":false},"cache_read_input_tokens":{"known":false},"output_tokens":{"known":false},"thinking_tokens":{"known":false}}}}`
	if got := string(envelope.Payload()); got != want {
		t.Fatalf("Anthropic native commit golden changed:\n%s", got)
	}
}

func TestNativeCommitRejectsIncompatibleAndCorruptPayloads(t *testing.T) {
	t.Parallel()
	validPayload := json.RawMessage(`{"kind":"sample","assistant":{"role":"assistant","content":[{"type":"text","text":"world"}]},"metadata":{"id":"msg-1","model":"claude-test","stop_reason":{"known":false},"usage":{"input_tokens":{"known":false},"cache_creation_input_tokens":{"known":false},"cache_read_input_tokens":{"known":false},"output_tokens":{"known":false},"thinking_tokens":{"known":false}}}}`)
	fixtures := []struct {
		name    string
		family  domain.ProviderFamily
		wire    string
		payload json.RawMessage
	}{
		{name: "family", family: domain.ProviderOpenAI, wire: messagesWire, payload: validPayload},
		{name: "wire", family: domain.ProviderAnthropic, wire: "completions", payload: validPayload},
		{name: "unexpected version field", family: domain.ProviderAnthropic, wire: messagesWire, payload: append([]byte(`{"payload_version":3,`), validPayload[1:]...)},
		{name: "kind", family: domain.ProviderAnthropic, wire: messagesWire, payload: bytes.Replace(validPayload, []byte(`"sample"`), []byte(`"future"`), 1)},
		{name: "role", family: domain.ProviderAnthropic, wire: messagesWire, payload: bytes.Replace(validPayload, []byte(`"role":"assistant"`), []byte(`"role":"user"`), 1)},
		{name: "unknown outer", family: domain.ProviderAnthropic, wire: messagesWire, payload: bytes.Replace(validPayload, []byte(`{"kind"`), []byte(`{"future":true,"kind"`), 1)},
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

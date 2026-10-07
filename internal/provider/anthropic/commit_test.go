package anthropic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

func TestNativeCommitRoundTripPreservesThinkingRawAndUsageKnowledge(t *testing.T) {
	t.Parallel()
	entry := nativeHistoryEntry{
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("question")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
			{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"},
			{Type: blockTypeRedactedThinking, RedactedData: "opaque-redacted", Raw: json.RawMessage(`{"type":"redacted_thinking","data":"opaque-redacted","future":true}`)},
			{Type: blockTypeText, Text: "answer"},
			{Type: "future_block", Raw: json.RawMessage(`{"type":"future_block","extension":{"enabled":true}}`)},
		}},
		Metadata: messageMetadata{
			ID: "msg-1", Model: "claude-test", StopReason: optionalString{Known: true, Value: "end_turn"},
			Usage: rawUsage{
				InputTokens:  optionalUint{Known: true, Value: 0},
				OutputTokens: optionalUint{Known: true, Value: 7},
			},
		},
	}
	envelope, err := encodeNativeCommit(entry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeCommit(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Input.Content[0].Text != "question" || len(decoded.Assistant.Content) != 4 ||
		decoded.Assistant.Content[0].Signature != "opaque-signature" ||
		!bytes.Equal(decoded.Assistant.Content[1].Raw, entry.Assistant.Content[1].Raw) ||
		!bytes.Equal(decoded.Assistant.Content[3].Raw, entry.Assistant.Content[3].Raw) {
		t.Fatalf("decoded turn = %#v", decoded)
	}
	usage := decoded.Metadata.Usage
	if !usage.InputTokens.Known || usage.InputTokens.Value != 0 ||
		usage.CacheCreationInputTokens.Known || usage.CacheReadInputTokens.Known ||
		!usage.OutputTokens.Known || usage.OutputTokens.Value != 7 {
		t.Fatalf("decoded usage = %#v", usage)
	}
	decoded.Assistant.Content[1].Raw[0] = '['
	if entry.Assistant.Content[1].Raw[0] != '{' || envelope.Payload()[0] != '{' {
		t.Fatal("round trip shares mutable opaque buffers")
	}
}

func TestNativeCommitCanonicalGolden(t *testing.T) {
	t.Parallel()
	envelope, err := encodeNativeCommit(nativeHistoryEntry{
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("hello")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "world"}}},
		Metadata: messageMetadata{
			ID: "msg-1", Model: "claude-test", StopReason: optionalString{Known: true, Value: "end_turn"},
			Usage: rawUsage{InputTokens: optionalUint{Known: true, Value: 3}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"kind":"sample","input":{"role":"user","content":[{"type":"text","text":"hello"}]},"assistant":{"role":"assistant","content":[{"type":"text","text":"world"}]},"metadata":{"id":"msg-1","model":"claude-test","stop_reason":{"known":true,"value":"end_turn"},"usage":{"input_tokens":{"known":true,"value":3},"cache_creation_input_tokens":{"known":false},"cache_read_input_tokens":{"known":false},"output_tokens":{"known":false}}}}`
	if got := string(envelope.Payload()); got != want {
		t.Fatalf("Anthropic native commit golden changed:\n%s", got)
	}
}

func TestNativeCommitRejectsIncompatibleAndCorruptPayloads(t *testing.T) {
	t.Parallel()
	validPayload := json.RawMessage(`{"kind":"sample","input":{"role":"user","content":[{"type":"text","text":"hello"}]},"assistant":{"role":"assistant","content":[{"type":"text","text":"world"}]},"metadata":{"id":"msg-1","model":"claude-test","stop_reason":{"known":false},"usage":{"input_tokens":{"known":false},"cache_creation_input_tokens":{"known":false},"cache_read_input_tokens":{"known":false},"output_tokens":{"known":false}}}}`)
	fixtures := []struct {
		name    string
		family  domain.ProviderFamily
		wire    string
		version int
		payload json.RawMessage
	}{
		{name: "family", family: domain.ProviderOpenAI, wire: messagesWire, version: 1, payload: validPayload},
		{name: "wire", family: domain.ProviderAnthropic, wire: "completions", version: 1, payload: validPayload},
		{name: "revision", family: domain.ProviderAnthropic, wire: messagesWire, version: 2, payload: validPayload},
		{name: "kind", family: domain.ProviderAnthropic, wire: messagesWire, version: 1, payload: bytes.Replace(validPayload, []byte(`"sample"`), []byte(`"future"`), 1)},
		{name: "role", family: domain.ProviderAnthropic, wire: messagesWire, version: 1, payload: bytes.Replace(validPayload, []byte(`"role":"user"`), []byte(`"role":"assistant"`), 1)},
		{name: "unknown outer", family: domain.ProviderAnthropic, wire: messagesWire, version: 1, payload: bytes.Replace(validPayload, []byte(`{"kind"`), []byte(`{"future":true,"kind"`), 1)},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			envelope, err := provider.NewNativeCommitEnvelope(fixture.family, fixture.wire, fixture.version, fixture.payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeNativeCommit(envelope); err == nil || strings.Contains(err.Error(), "hello") {
				t.Fatalf("decodeNativeCommit() error = %v", err)
			}
		})
	}
}

func TestNativeCommitRejectsOversizedPayload(t *testing.T) {
	t.Parallel()
	entry := nativeHistoryEntry{
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("hello")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{
			Type: "future_block",
			Raw:  append(append(json.RawMessage(`{"type":"future_block","data":"`), bytes.Repeat([]byte{'x'}, provider.MaxNativeCommitBytes)...), []byte(`"}`)...),
		}}},
		Metadata: messageMetadata{ID: "msg-1", Model: "claude-test"},
	}
	if _, err := encodeNativeCommit(entry); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("encodeNativeCommit() error = %v", err)
	}
}

package openai

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

func TestNativeCommitRoundTripPreservesPrivateResponsesItems(t *testing.T) {
	t.Parallel()
	turn := nativeTurn{
		User: NewUserItem("question"),
		Outputs: []NativeItem{
			{
				Type: "reasoning", ID: "reason-1", Phase: "analysis",
				ReasoningSummary: []ReasoningSummaryPart{{Type: "summary_text", Text: "summary"}},
				EncryptedContent: "opaque-encrypted",
			},
			{
				Type: "message", ID: "message-1", Role: "assistant", Phase: "final",
				Content: []ContentPart{{Type: "output_text", Text: "answer"}},
			},
			{
				Type: "future_item", ID: "future-1",
				Raw: json.RawMessage(`{"type":"future_item","id":"future-1","extension":{"enabled":true}}`),
			},
		},
	}
	envelope, err := encodeNativeCommit(turn)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNativeCommit(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.User.Content[0].Text != "question" || len(decoded.Outputs) != 3 ||
		decoded.Outputs[0].Phase != "analysis" || decoded.Outputs[0].EncryptedContent != "opaque-encrypted" ||
		decoded.Outputs[1].Phase != "final" || decoded.Outputs[1].Content[0].Text != "answer" ||
		!bytes.Equal(decoded.Outputs[2].Raw, turn.Outputs[2].Raw) {
		t.Fatalf("decoded turn = %#v", decoded)
	}
	decoded.Outputs[2].Raw[0] = '['
	if turn.Outputs[2].Raw[0] != '{' || envelope.Payload()[0] != '{' {
		t.Fatal("round trip shares mutable opaque buffers")
	}
}

func TestNativeCommitCanonicalGolden(t *testing.T) {
	t.Parallel()
	envelope, err := encodeNativeCommit(nativeTurn{
		User: NewUserItem("hello"),
		Outputs: []NativeItem{{
			Type: "message", ID: "msg-1", Role: "assistant", Phase: "final",
			Content: []ContentPart{{Type: "output_text", Text: "world"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"shape":"text_sample","user":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},"output_items":[{"type":"message","id":"msg-1","role":"assistant","phase":"final","content":[{"type":"output_text","text":"world"}]}]}`
	if got := string(envelope.Payload()); got != want {
		t.Fatalf("OpenAI native commit golden changed:\n%s", got)
	}
}

func TestNativeCommitRejectsIncompatibleAndCorruptPayloads(t *testing.T) {
	t.Parallel()
	validPayload := json.RawMessage(`{"shape":"text_sample","user":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},"output_items":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"world"}]}]}`)
	fixtures := []struct {
		name    string
		family  domain.ProviderFamily
		wire    string
		version int
		payload json.RawMessage
	}{
		{name: "family", family: domain.ProviderAnthropic, wire: responsesWire, version: 1, payload: validPayload},
		{name: "wire", family: domain.ProviderOpenAI, wire: "chat_completions", version: 1, payload: validPayload},
		{name: "revision", family: domain.ProviderOpenAI, wire: responsesWire, version: 2, payload: validPayload},
		{name: "shape", family: domain.ProviderOpenAI, wire: responsesWire, version: 1, payload: json.RawMessage(`{"shape":"future","user":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},"output_items":[{"type":"message","role":"assistant"}]}`)},
		{name: "missing outputs", family: domain.ProviderOpenAI, wire: responsesWire, version: 1, payload: json.RawMessage(`{"shape":"text_sample","user":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},"output_items":[]}`)},
		{name: "unknown outer", family: domain.ProviderOpenAI, wire: responsesWire, version: 1, payload: json.RawMessage(`{"shape":"text_sample","user":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},"output_items":[{"type":"message","role":"assistant"}],"future":true}`)},
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
	turn := nativeTurn{
		User: NewUserItem("hello"),
		Outputs: []NativeItem{{
			Type: "future_item",
			Raw:  append(append(json.RawMessage(`{"type":"future_item","data":"`), bytes.Repeat([]byte{'x'}, provider.MaxNativeCommitBytes)...), []byte(`"}`)...),
		}},
	}
	if _, err := encodeNativeCommit(turn); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("encodeNativeCommit() error = %v", err)
	}
}

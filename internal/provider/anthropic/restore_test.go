package anthropic

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestRestoreConversationMatchesUninterruptedNextRequest(t *testing.T) {
	t.Parallel()
	instance, err := New(Config{
		BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "claude-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	turns := []nativeTurn{
		{
			User: newUserMessage("first"),
			Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
				{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"},
				{Type: blockTypeText, Text: "answer-1"},
			}},
			Metadata: messageMetadata{ID: "msg-1", Model: "claude-test"},
		},
		{
			User: newUserMessage("second"),
			Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{
				Type: "future_block", Raw: json.RawMessage(`{"type":"future_block","opaque":{"value":2}}`),
			}}},
			Metadata: messageMetadata{
				ID: "msg-2", Model: "claude-test",
				Usage: rawUsage{InputTokens: optionalUint{Known: true, Value: 9}},
			},
		},
	}
	uninterrupted := &Conversation{provider: instance}
	commits := make([]provider.NativeCommitEnvelope, 0, len(turns))
	for _, turn := range turns {
		uninterrupted.history.commit(turn)
		commit, encodeErr := encodeNativeCommit(turn)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		commits = append(commits, commit)
	}
	restoredValue, err := instance.RestoreConversation(commits)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	uninterruptedFootprint, err := uninterrupted.HistoryFootprint()
	if err != nil {
		t.Fatal(err)
	}
	restoredFootprint, err := restored.HistoryFootprint()
	if err != nil {
		t.Fatal(err)
	}
	if restoredFootprint != uninterruptedFootprint {
		t.Fatalf("restored footprint differs: %#v %#v", restoredFootprint, uninterruptedFootprint)
	}
	uninterruptedView := uninterrupted.ProjectHistory()
	restoredView := restored.ProjectHistory()
	if !reflect.DeepEqual(restoredView, uninterruptedView) {
		t.Fatalf("restored projection differs: %#v %#v", restoredView, uninterruptedView)
	}
	restoredView.Turns[0].UserText = "mutated projection"
	next := newUserMessage("third")
	uninterruptedRequest, err := compileMessagesRequest(
		instance.config.Model, instance.config.MaxOutputTokens, uninterrupted.historySnapshot(), next,
	)
	if err != nil {
		t.Fatal(err)
	}
	restoredRequest, err := compileMessagesRequest(
		instance.config.Model, instance.config.MaxOutputTokens, restored.historySnapshot(), next,
	)
	if err != nil {
		t.Fatal(err)
	}
	uninterruptedBytes := uninterruptedRequest.Bytes()
	restoredBytes := restoredRequest.Bytes()
	if !bytes.Equal(restoredBytes, uninterruptedBytes) {
		t.Fatalf("restored request differs:\n%s\n%s", restoredBytes, uninterruptedBytes)
	}
	first, err := contextplan.NewSegment("anthropic-messages-request", contextplan.StabilityTurnStable, "v1", uninterruptedRequest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := contextplan.NewSegment("anthropic-messages-request", contextplan.StabilityTurnStable, "v1", restoredRequest)
	if err != nil {
		t.Fatal(err)
	}
	restoredShape := buildMessagesRequest(instance.config.Model, instance.config.MaxOutputTokens, restored.historySnapshot(), next)
	if first.Fingerprint() != second.Fingerprint() || len(restoredShape.Messages) != 5 ||
		restoredShape.Messages[3].Content[0].Type != "future_block" {
		t.Fatalf("fingerprints/order differ: %q %q %#v", first.Fingerprint(), second.Fingerprint(), restoredShape.Messages)
	}
}

func TestRestoreConversationRejectsEntireHistoryOnOneBadCommit(t *testing.T) {
	t.Parallel()
	instance, err := New(Config{
		BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "claude-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	valid, err := encodeNativeCommit(nativeTurn{
		User:      newUserMessage("first"),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "answer"}}},
		Metadata:  messageMetadata{ID: "msg-1", Model: "claude-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := provider.NewNativeCommitEnvelope(
		domain.ProviderAnthropic, messagesWire, 1,
		json.RawMessage(`{"shape":"text_sample","user":{"role":"assistant","content":[]},"assistant":{"role":"assistant","content":[]},"metadata":{"id":"msg-2","model":"claude-test","stop_reason":{"known":false},"usage":{"input_tokens":{"known":false},"cache_creation_input_tokens":{"known":false},"cache_read_input_tokens":{"known":false},"output_tokens":{"known":false}}}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := instance.RestoreConversation([]provider.NativeCommitEnvelope{valid, bad})
	if err == nil || restored != nil {
		t.Fatalf("RestoreConversation() = %#v, %v", restored, err)
	}
	if strings.Contains(err.Error(), "first") || strings.Contains(err.Error(), "claude-test") {
		t.Fatalf("restore error exposed payload: %v", err)
	}
}

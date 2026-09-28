package openai

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestRestoreConversationMatchesUninterruptedNextRequest(t *testing.T) {
	t.Parallel()
	instance, err := New(Config{
		BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "gpt-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	turns := []nativeTurn{
		{
			User: NewUserItem("first"),
			Outputs: []NativeItem{
				{Type: "reasoning", ID: "reason-1", EncryptedContent: "opaque-1"},
				{Type: "message", ID: "message-1", Role: "assistant", Phase: "final", Content: []ContentPart{{Type: "output_text", Text: "answer-1"}}},
			},
		},
		{
			User: NewUserItem("second"),
			Outputs: []NativeItem{{
				Type: "future_item", Raw: json.RawMessage(`{"type":"future_item","id":"future-2","opaque":{"value":2}}`),
			}},
		},
	}
	uninterrupted := &Conversation{provider: instance}
	commits := make([]provider.NativeCommitEnvelope, 0, len(turns))
	for _, turn := range turns {
		uninterrupted.history.commit(turn.User, turn.Outputs)
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
	uninterruptedView := uninterrupted.ProjectHistory()
	restoredView := restored.ProjectHistory()
	if !reflect.DeepEqual(restoredView, uninterruptedView) {
		t.Fatalf("restored projection differs: %#v %#v", restoredView, uninterruptedView)
	}
	restoredView.Turns[0].UserText = "mutated projection"

	next := NewUserItem("third")
	uninterruptedRequest := compileResponsesRequest(instance.config.Model, uninterrupted.historySnapshot(), next)
	restoredRequest := compileResponsesRequest(instance.config.Model, restored.historySnapshot(), next)
	uninterruptedBytes, err := codec.MarshalStable(uninterruptedRequest)
	if err != nil {
		t.Fatal(err)
	}
	restoredBytes, err := codec.MarshalStable(restoredRequest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredBytes, uninterruptedBytes) {
		t.Fatalf("restored request differs:\n%s\n%s", restoredBytes, uninterruptedBytes)
	}
	first, err := contextplan.NewSegment("openai-responses-request", contextplan.StabilityTurnStable, "v1", uninterruptedRequest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := contextplan.NewSegment("openai-responses-request", contextplan.StabilityTurnStable, "v1", restoredRequest)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint || len(restoredRequest.Input) != 6 || restoredRequest.Input[4].Type != "future_item" {
		t.Fatalf("fingerprints/order differ: %q %q %#v", first.Fingerprint, second.Fingerprint, restoredRequest.Input)
	}
}

func TestRestoreConversationRejectsEntireHistoryOnOneBadCommit(t *testing.T) {
	t.Parallel()
	instance, err := New(Config{
		BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "gpt-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	valid, err := encodeNativeCommit(nativeTurn{
		User:    NewUserItem("first"),
		Outputs: []NativeItem{{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "answer"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, responsesWire, 1,
		json.RawMessage(`{"shape":"text_sample","user":{"type":"message","role":"assistant"},"output_items":[]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := instance.RestoreConversation([]provider.NativeCommitEnvelope{valid, bad})
	if err == nil || restored != nil {
		t.Fatalf("RestoreConversation() = %#v, %v", restored, err)
	}
	if strings.Contains(err.Error(), "first") || strings.Contains(err.Error(), "assistant") {
		t.Fatalf("restore error exposed payload: %v", err)
	}
}

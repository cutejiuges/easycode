package anthropic

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
		BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "claude-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	entries := []nativeHistoryEntry{
		{
			Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("first")),
			Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
				{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"},
				{Type: blockTypeText, Text: "answer-1"},
			}},
			Metadata: messageMetadata{ID: "msg-1", Model: "claude-test"},
		},
		{
			Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("second")),
			Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{
				Type: "future_block", Raw: json.RawMessage(`{"type":"future_block","opaque":{"value":2}}`),
			}}},
			Metadata: messageMetadata{
				ID: "msg-2", Model: "claude-test",
				Usage: rawUsage{InputTokens: optionalUint{Known: true, Value: 9}},
			},
		},
		{
			Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("search")),
			Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
				{Type: blockTypeToolUse, ID: "toolu-glob", Name: "Glob", Input: json.RawMessage(`{"pattern":"**/*.go"}`)},
				{Type: blockTypeToolUse, ID: "toolu-grep", Name: "Grep", Input: json.RawMessage(`{"pattern":"TODO"}`)},
				{Type: blockTypeToolUse, ID: "toolu-read", Name: "Read", Input: json.RawMessage(`{"file_path":"README.md"}`)},
			}},
			Metadata: messageMetadata{ID: "msg-search", Model: "claude-test"},
		},
		{
			Kind: nativeHistoryToolOutputs,
			ToolOutputs: nativeMessage{Role: roleUser, Content: []NativeItem{
				{Type: blockTypeToolResult, ToolUseID: "toolu-glob", Content: "frozen glob"},
				{Type: blockTypeToolResult, ToolUseID: "toolu-grep", Content: "frozen grep"},
				{Type: blockTypeToolResult, ToolUseID: "toolu-read", Content: "frozen read"},
			}},
		},
	}
	uninterrupted := &Conversation{provider: instance}
	commits := make([]provider.NativeCommitEnvelope, 0, len(entries))
	for _, entry := range entries {
		uninterrupted.history.commit(entry)
		commit, encodeErr := encodeNativeCommit(entry)
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
	projectInstructions := testAnthropicProjectInstructions(t, "AGENTS.md", "same-startup-snapshot")
	uninterruptedToolView := testAnthropicToolView(t)
	restoredToolView := testAnthropicToolView(t)
	if uninterruptedToolView.Fingerprint() != restoredToolView.Fingerprint() {
		t.Fatal("restored tool catalog fingerprint differs")
	}
	uninterruptedRequest, err := compileMessagesRequest(
		instance.config.Model, instance.config.MaxOutputTokens, uninterrupted.historySnapshot(), &projectInstructions, nativeMessagePointer(next), uninterruptedToolView,
	)
	if err != nil {
		t.Fatal(err)
	}
	restoredRequest, err := compileMessagesRequest(
		instance.config.Model, instance.config.MaxOutputTokens, restored.historySnapshot(), &projectInstructions, nativeMessagePointer(next), restoredToolView,
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
	restoredShape := buildMessagesRequest(instance.config.Model, instance.config.MaxOutputTokens, restored.historySnapshot(), &projectInstructions, nativeMessagePointer(next), restoredToolView)
	if first.Fingerprint() != second.Fingerprint() || len(restoredShape.Messages) != 9 ||
		restoredShape.Messages[0].Content[0].Text != projectInstructions.RenderedText() ||
		restoredShape.Messages[4].Content[0].Type != "future_block" ||
		restoredShape.Messages[6].Content[0].ID != "toolu-glob" || restoredShape.Messages[6].Content[1].ID != "toolu-grep" ||
		restoredShape.Messages[6].Content[2].ID != "toolu-read" || restoredShape.Messages[7].Content[0].ToolUseID != "toolu-glob" ||
		restoredShape.Messages[7].Content[1].ToolUseID != "toolu-grep" || restoredShape.Messages[7].Content[2].ToolUseID != "toolu-read" ||
		restoredShape.Messages[7].Content[0].Content != "frozen glob" || restoredShape.Messages[7].Content[1].Content != "frozen grep" ||
		restoredShape.Messages[7].Content[2].Content != "frozen read" {
		t.Fatalf("fingerprints/order differ: %q %q %#v", first.Fingerprint(), second.Fingerprint(), restoredShape.Messages)
	}

	changedProjectInstructions := testAnthropicProjectInstructions(t, "AGENTS.md", "changed-startup-snapshot")
	changedRequest, err := compileMessagesRequest(
		instance.config.Model, instance.config.MaxOutputTokens, restored.historySnapshot(), &changedProjectInstructions, nativeMessagePointer(next), testAnthropicToolView(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	changedShape := buildMessagesRequest(
		instance.config.Model, instance.config.MaxOutputTokens, restored.historySnapshot(), &changedProjectInstructions, nativeMessagePointer(next), testAnthropicToolView(t),
	)
	if bytes.Equal(changedRequest.Bytes(), restoredRequest.Bytes()) ||
		!reflect.DeepEqual(changedShape.Messages[1:], restoredShape.Messages[1:]) ||
		changedShape.Messages[0].Content[0].Text == restoredShape.Messages[0].Content[0].Text {
		t.Fatalf("changed project instructions modified native history: %#v %#v", restoredShape.Messages, changedShape.Messages)
	}
	changedFootprint, err := restored.HistoryFootprint()
	if err != nil || changedFootprint != restoredFootprint {
		t.Fatalf("project instruction change modified footprint: %#v, %v", changedFootprint, err)
	}
	originalProjectJSON, err := codec.MarshalCanonical(json.RawMessage(projectInstructions.CanonicalJSON()), contextplan.MaxSegmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	originalProjectSegment, err := contextplan.NewSegment(
		"project_instructions", contextplan.StabilityProjectStable, projectInstructions.Revision(), originalProjectJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	changedProjectJSON, err := codec.MarshalCanonical(json.RawMessage(changedProjectInstructions.CanonicalJSON()), contextplan.MaxSegmentBytes)
	if err != nil {
		t.Fatal(err)
	}
	changedProjectSegment, err := contextplan.NewSegment(
		"project_instructions", contextplan.StabilityProjectStable, changedProjectInstructions.Revision(), changedProjectJSON,
	)
	if err != nil {
		t.Fatal(err)
	}
	if originalProjectSegment.Fingerprint() == changedProjectSegment.Fingerprint() {
		t.Fatal("changed project instructions did not change the project-stable fingerprint")
	}
	for _, commit := range commits {
		if bytes.Contains(commit.Payload(), []byte("startup-snapshot")) {
			t.Fatalf("native commit contains project instructions: %s", commit.Payload())
		}
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
	valid, err := encodeNativeCommit(nativeHistoryEntry{
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("first")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "answer"}}},
		Metadata:  messageMetadata{ID: "msg-1", Model: "claude-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := provider.NewNativeCommitEnvelope(
		domain.ProviderAnthropic, messagesWire, 1,
		json.RawMessage(`{"kind":"sample","input":{"role":"assistant","content":[]},"assistant":{"role":"assistant","content":[]},"metadata":{"id":"msg-2","model":"claude-test","stop_reason":{"known":false},"usage":{"input_tokens":{"known":false},"cache_creation_input_tokens":{"known":false},"cache_read_input_tokens":{"known":false},"output_tokens":{"known":false}}}}`),
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

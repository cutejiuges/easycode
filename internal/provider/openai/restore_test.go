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
	entries := []nativeHistoryEntry{
		{
			Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("first")),
			Outputs: []NativeItem{
				{Type: "reasoning", ID: "reason-1", EncryptedContent: "opaque-1"},
				{Type: "message", ID: "message-1", Role: "assistant", Phase: "final", Content: []ContentPart{{Type: "output_text", Text: "answer-1"}}},
			},
		},
		{
			Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("second")),
			Outputs: []NativeItem{{
				Type: "future_item", Raw: json.RawMessage(`{"type":"future_item","id":"future-2","opaque":{"value":2}}`),
			}},
			Usage: rawUsage{
				InputTokens:       optionalUint{Known: true, Value: 12},
				CachedInputTokens: optionalUint{Known: true, Value: 4},
				OutputTokens:      optionalUint{Known: true, Value: 7},
			},
		},
		{
			Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("search")),
			Outputs: []NativeItem{
				{Type: "function_call", ID: "fc-glob", CallID: "call-glob", Name: "Glob", Arguments: `{"pattern":"**/*.go"}`},
				{Type: "function_call", ID: "fc-grep", CallID: "call-grep", Name: "Grep", Arguments: `{"pattern":"TODO"}`},
				{Type: "function_call", ID: "fc-read", CallID: "call-read", Name: "Read", Arguments: `{"file_path":"README.md"}`},
			},
		},
		{
			Kind: nativeHistoryToolOutputs,
			ToolOutputs: []NativeItem{
				{Type: "function_call_output", CallID: "call-glob", Output: `{"status":"success","code":"ok","content":"frozen glob"}`},
				{Type: "function_call_output", CallID: "call-grep", Output: `{"status":"success","code":"ok","content":"frozen grep"}`},
				{Type: "function_call_output", CallID: "call-read", Output: `{"status":"success","code":"ok","content":"frozen read"}`},
			},
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

	next := NewUserItem("third")
	projectInstructions := testOpenAIProjectInstructions(t, "AGENTS.md", "same-startup-snapshot")
	uninterruptedToolView := testOpenAIToolView(t)
	restoredToolView := testOpenAIToolView(t)
	if uninterruptedToolView.Fingerprint() != restoredToolView.Fingerprint() {
		t.Fatal("restored tool catalog fingerprint differs")
	}
	uninterruptedRequest, err := compileResponsesRequest(instance.config.Model, uninterrupted.historySnapshot(), &projectInstructions, nativeItemPointer(next), uninterruptedToolView)
	if err != nil {
		t.Fatal(err)
	}
	restoredRequest, err := compileResponsesRequest(instance.config.Model, restored.historySnapshot(), &projectInstructions, nativeItemPointer(next), restoredToolView)
	if err != nil {
		t.Fatal(err)
	}
	uninterruptedBytes := uninterruptedRequest.Bytes()
	restoredBytes := restoredRequest.Bytes()
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
	restoredShape := buildResponsesRequest(instance.config.Model, restored.historySnapshot(), &projectInstructions, nativeItemPointer(next), restoredToolView)
	if first.Fingerprint() != second.Fingerprint() || len(restoredShape.Input) != 14 ||
		restoredShape.Input[0].Content[0].Text != projectInstructions.RenderedText() || restoredShape.Input[5].Type != "future_item" ||
		restoredShape.Input[7].CallID != "call-glob" || restoredShape.Input[8].CallID != "call-grep" || restoredShape.Input[9].CallID != "call-read" ||
		restoredShape.Input[10].CallID != "call-glob" || restoredShape.Input[11].CallID != "call-grep" || restoredShape.Input[12].CallID != "call-read" ||
		!strings.Contains(restoredShape.Input[10].Output, "frozen glob") || !strings.Contains(restoredShape.Input[11].Output, "frozen grep") ||
		!strings.Contains(restoredShape.Input[12].Output, "frozen read") {
		t.Fatalf("fingerprints/order differ: %q %q %#v", first.Fingerprint(), second.Fingerprint(), restoredShape.Input)
	}

	changedProjectInstructions := testOpenAIProjectInstructions(t, "AGENTS.md", "changed-startup-snapshot")
	changedRequest, err := compileResponsesRequest(
		instance.config.Model, restored.historySnapshot(), &changedProjectInstructions, nativeItemPointer(next), testOpenAIToolView(t),
	)
	if err != nil {
		t.Fatal(err)
	}
	changedShape := buildResponsesRequest(
		instance.config.Model, restored.historySnapshot(), &changedProjectInstructions, nativeItemPointer(next), testOpenAIToolView(t),
	)
	if bytes.Equal(changedRequest.Bytes(), restoredRequest.Bytes()) ||
		!reflect.DeepEqual(changedShape.Input[1:], restoredShape.Input[1:]) ||
		changedShape.Input[0].Content[0].Text == restoredShape.Input[0].Content[0].Text {
		t.Fatalf("changed project instructions modified native history: %#v %#v", restoredShape.Input, changedShape.Input)
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
		BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "gpt-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	valid, err := encodeNativeCommit(nativeHistoryEntry{
		Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("first")),
		Outputs: []NativeItem{{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "answer"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, responsesWire, 1,
		json.RawMessage(`{"kind":"sample","input":{"type":"message","role":"assistant"},"outputs":[]}`),
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

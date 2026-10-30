package anthropic

import (
	"bytes"
	"crypto/sha256"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"os"
	"strings"
	"testing"
)

func TestTranscriptOrderPrivacyAndRequest(t *testing.T) {
	c := &Conversation{}
	testCommitHistory(t, &c.history, testInputEntry(t, newUserMessage("question")), nativeHistoryEntry{Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
		{Type: blockTypeThinking, Thinking: "private-thought", Signature: "private-signature"},
		{Type: blockTypeText, Text: " before\n"},
		{Type: blockTypeToolUse, ID: "call-1", Name: "Read", Input: []byte(`{"file_path":"x"}`)},
		{Type: blockTypeText, Text: " after "},
		{Type: blockTypeToolUse, ID: "call-2", Name: "Edit", Input: []byte(`{"file_path":"x","old_string":"a","new_string":"b"}`)},
	}}})
	before, err := compileMessagesRequest("test-model", DefaultMaxOutputTokens, c.history.snapshot(), nil, testAnthropicToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	view, err := c.ProjectTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 5 || view.Items[1].Text != " before\n" || view.Items[2].CallID != "call-1" || view.Items[3].Text != " after " || view.Items[4].CallID != "call-2" {
		t.Fatalf("order: %#v", view)
	}
	data, err := codec.MarshalStable(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private") || strings.Contains(string(data), "file_path") {
		t.Fatal("private fields leaked")
	}
	golden, err := os.ReadFile("testdata/transcript_projection.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, bytes.TrimSpace(golden)) {
		t.Fatalf("golden mismatch:\n%s", data)
	}
	view.Items[1].Text = "changed"
	next, err := c.ProjectTranscript()
	if err != nil || next.Items[1].Text != " before\n" {
		t.Fatal("projection shares mutable storage")
	}
	after, err := compileMessagesRequest("test-model", DefaultMaxOutputTokens, c.history.snapshot(), nil, testAnthropicToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Bytes(), after.Bytes()) || sha256.Sum256(before.Bytes()) != sha256.Sum256(after.Bytes()) {
		t.Fatal("projection changed request")
	}
	if c.ProjectHistory().Turns[0].Messages[1].Text != " before\n after " {
		t.Fatal("semantic history changed")
	}
}
func TestTranscriptRejectsMissingAssociation(t *testing.T) {
	if _, err := projectTranscript([]nativeHistoryEntry{{Kind: nativeHistoryUserInputs}}); err == nil {
		t.Fatal("accepted missing input association")
	}
	_, err := domain.NewTranscriptView(domain.ProviderOpenAI, []domain.TranscriptItem{})
	if err != nil {
		t.Fatal(err)
	}
}

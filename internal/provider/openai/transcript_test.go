package openai

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
	testCommitHistory(t, &c.history, testInputEntry(t, NewUserItem("question")), nativeHistoryEntry{Kind: nativeHistorySample, Outputs: []NativeItem{
		{Type: "reasoning", EncryptedContent: "private-encrypted", Phase: "private-phase"},
		{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: " before\n"}}},
		{Type: "function_call", CallID: "call-1", Name: "Read", Arguments: `{"file_path":"x"}`},
		{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: " after "}}},
		{Type: "custom_tool_call", CallID: "call-2", Name: "apply_patch", Input: "private-arguments"},
	}})
	before, err := compileResponsesRequest("test-model", c.history.snapshot(), nil, testOpenAIToolView(t))
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
	after, err := compileResponsesRequest("test-model", c.history.snapshot(), nil, testOpenAIToolView(t))
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

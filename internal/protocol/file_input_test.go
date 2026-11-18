package protocol

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func protocolFiles(t *testing.T) domain.FileAttachments {
	t.Helper()
	data, err := os.ReadFile("../domain/testdata/file_attachment.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := domain.DecodeFileAttachment(data)
	if err != nil {
		t.Fatal(err)
	}
	files, err := domain.NewFileAttachments([]domain.FileAttachment{a})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
func TestInteractiveFileDraftRequiresCompletePreparationBeforeAdmission(t *testing.T) {
	text := "inspect @src/a.go#L2-8"
	draft, err := domain.NewInteractiveInputDraft(text)
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewSubmitConversationalInputDraftCommand(testRequestID, draft)
	if err != nil || c.Validate() != nil || c.ValidateForAdmission() == nil {
		t.Fatal("unprepared command reached admission", err)
	}
	files := protocolFiles(t)
	c, err = c.WithFileAttachments(files)
	if err != nil || c.ValidateForAdmission() != nil || c.Text() != text || c.InputDraft().Text() != text || c.FileAttachments().Len() != 1 || c.InputByteSize() != len(text)+files.ByteSize() {
		t.Fatal("prepared draft changed or undercounted", err)
	}
	literal, _ := NewSubmitInputCommand(testRequestID, text)
	if literal.ValidateForAdmission() != nil || literal.InputDraft().Interactive() || literal.FileAttachments().Len() != 0 {
		t.Fatal("headless @ was interpreted")
	}
	for _, makeCommand := range []func() (Command, error){func() (Command, error) { return NewShutdownCommand(testRequestID) }, func() (Command, error) { return NewQueryContextCommand(testRequestID) }, func() (Command, error) { return NewInterruptCommand(testRequestID, testTurnID) }} {
		other, _ := makeCommand()
		if _, err := other.WithFileAttachments(files); err == nil {
			t.Fatal("unrelated command accepted file payload")
		}
		other.inputDraft = draft
		if other.Validate() == nil {
			t.Fatal("foreign draft accepted")
		}
	}
	large, _ := domain.NewInteractiveInputDraft(strings.Repeat("x", MaxInputBytes-len(text)) + text)
	c, _ = NewSubmitConversationalInputDraftCommand(testRequestID, large)
	if _, err := c.WithFileAttachments(files); err == nil {
		t.Fatal("metadata bytes escaped 4 MiB limit")
	}
}

func TestFileCommitSummaryStrictRequiredAndBodyFree(t *testing.T) {
	input, _ := domain.NewUserInput(domain.InputID(testTurnID), "inspect @src/a.go#L2-8")
	input, err := input.WithFileAttachments(protocolFiles(t))
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewUserInputCommitted(input)
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeUserInputCommitted(e)
	if err != nil || p.Files.Total() != 1 || len(p.Files.Items()) != 1 {
		t.Fatal(err)
	}
	if bytes.Contains(e.Payload, []byte("second")) || bytes.Contains(e.Payload, []byte("digest")) {
		t.Fatal("commit event carried file body or private digest")
	}
	for _, payload := range [][]byte{bytes.Replace(e.Payload, []byte(`"files":`), []byte(`"unknown_files":`), 1), bytes.Replace(e.Payload, []byte(`"total":1`), []byte(`"total":2`), 1), bytes.Replace(e.Payload, []byte(`"omitted":0`), []byte(`"omitted":1`), 1), bytes.Replace(e.Payload, []byte(`"src/a.go"`), []byte(`"src/b.go"`), 1)} {
		bad := e
		bad.Payload = payload
		if _, err := DecodeUserInputCommitted(bad); err == nil {
			t.Fatalf("forged summary accepted: %s", payload)
		}
	}
	encoded, err := codec.MarshalStable(p)
	if err != nil || !bytes.Equal(encoded, e.Payload) {
		t.Fatal("summary decode changed canonical bytes", err)
	}
	plain, _ := domain.NewUserInput(domain.InputID(testTurnID), "literal @missing")
	e, err = NewUserInputCommitted(plain)
	if err != nil {
		t.Fatal(err)
	}
	p, err = DecodeUserInputCommitted(e)
	if err != nil || p.Files.Total() != 0 {
		t.Fatal("literal input required file metadata", err)
	}
}

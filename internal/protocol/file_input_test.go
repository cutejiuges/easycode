package protocol

import (
	"bytes"
	"os"
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

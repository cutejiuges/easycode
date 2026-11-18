package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func fileRef(t *testing.T, path string, selection FileSelection) FileReference {
	t.Helper()
	ref, err := NewFileReference(path, selection)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestFileAttachmentOriginalBytesRangesAndGolden(t *testing.T) {
	for _, source := range []string{"", "one", "one\n", "\n", "\n\n", "\ufeffone\r\ntwo\r\n", "one\rtwo", "one\n\nlast"} {
		a, err := FreezeFileAttachment(fileRef(t, "a", NewWholeFileSelection()), source)
		if err != nil || a.Content() != source || a.TotalLines() != countFileLines(source) || a.Truncated() || a.EndsMidLine() {
			t.Fatalf("source %q: %#v %v", source, a, err)
		}
		encoded, err := codec.MarshalStable(a)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeFileAttachment(encoded)
		if err != nil || decoded != a {
			t.Fatalf("decode: %v", err)
		}
	}
	selection, _ := NewLineFileSelection(2, 8)
	a, err := FreezeFileAttachment(fileRef(t, "src/a.go", selection), "first\r\nsecond\nlast")
	if err != nil || a.Content() != "second\nlast" || a.TotalLines() != 3 || a.ActualStartLine() != 2 || a.ActualEndLine() != 3 || a.Truncated() {
		t.Fatalf("clamped range: %#v %v", a, err)
	}
	rendered, _ := a.RenderedText()
	expected, err := os.ReadFile("testdata/file_attachment.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(rendered), expected) {
		t.Fatalf("golden mismatch\n%s", rendered)
	}
	encoded, _ := codec.MarshalStable(a)
	record, err := os.ReadFile("testdata/file_attachment.json")
	if err != nil || !bytes.Equal(encoded, bytes.TrimSuffix(record, []byte("\n"))) {
		t.Fatal("fixed metadata differs", err)
	}
	afterEOF, _ := NewLineFileSelection(4, 4)
	if _, err := FreezeFileAttachment(fileRef(t, "a", afterEOF), "one\ntwo\nthree\n"); err == nil {
		t.Fatal("EOF range accepted")
	}
}

func TestFileAttachmentLimitsAndPartialLine(t *testing.T) {
	for _, source := range []string{strings.Repeat("line\n", 201), strings.Repeat("界\"\\\r\n", 30000), strings.Repeat("\"\\\t<&界", 30000), strings.Repeat("\u2028\u2029", 30000)} {
		a, err := FreezeFileAttachment(fileRef(t, "a", NewWholeFileSelection()), source)
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := a.RenderedText()
		if err != nil || len(rendered) > MaxFileRenderedBytes || !a.Truncated() || !strings.HasPrefix(source, a.Content()) {
			t.Fatalf("bounded prefix: %v", err)
		}
		if strings.HasSuffix(a.Content(), "\r") && strings.HasPrefix(source[len(a.Content()):], "\n") {
			t.Fatal("split CRLF")
		}
		if !strings.HasSuffix(a.Content(), "\n") && !a.EndsMidLine() {
			t.Fatal("partial line hidden")
		}
	}
	rangeSelection, _ := NewLineFileSelection(1, 3000)
	a, err := FreezeFileAttachment(fileRef(t, "a", rangeSelection), strings.Repeat("x\n", 3000))
	if err != nil || a.ActualEndLine() != 2000 || !a.Truncated() || a.EndsMidLine() {
		t.Fatal("line cap", err)
	}
	for _, source := range []string{"a\x00b", string([]byte{0xff}), strings.Repeat("a", MaxFileSourceBytes+1)} {
		if _, err := FreezeFileAttachment(fileRef(t, "a", NewWholeFileSelection()), source); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestFileAttachmentRejectsForgedSemanticsEvenWithValidDigest(t *testing.T) {
	a, err := FreezeFileAttachment(fileRef(t, "a", NewWholeFileSelection()), "one\ntwo\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*fileAttachmentContent){
		func(v *fileAttachmentContent) { v.TotalLines = 3 },
		func(v *fileAttachmentContent) { v.ActualStartLine = 2 },
		func(v *fileAttachmentContent) { v.ActualEndLine = 1 },
		func(v *fileAttachmentContent) { v.EndsMidLine = true },
		func(v *fileAttachmentContent) { v.Truncated = true },
		func(v *fileAttachmentContent) { v.Path = "./a" },
	} {
		forged := a
		mutate(&forged.value)
		data, _ := codec.MarshalStable(forged.value)
		sum := sha256.Sum256(data)
		forged.digest = hex.EncodeToString(sum[:])
		if forged.Validate() == nil {
			t.Fatal("valid digest hid invalid semantics")
		}
	}
	encoded, _ := codec.MarshalStable(a)
	for _, bad := range [][]byte{[]byte("null"), []byte("{}"), bytes.Replace(encoded, []byte(`"total_lines":2`), []byte(`"total_lines":null`), 1), bytes.Replace(encoded, []byte(`"ends_mid_line":false,`), nil, 1), bytes.Replace(encoded, []byte(`"path":"a"`), []byte(`"path":"a","path":"b"`), 1), bytes.Replace(encoded, []byte(`"content":`), []byte(`"extra":0,"content":`), 1)} {
		if _, err := DecodeFileAttachment(bad); err == nil {
			t.Fatalf("invalid metadata accepted: %s", bad)
		}
	}
}

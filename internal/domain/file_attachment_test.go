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

func TestFileAttachmentsFreezeUserInputAndIsolateCopies(t *testing.T) {
	a, _ := FreezeFileAttachment(fileRef(t, "a", NewWholeFileSelection()), "/effort high\n@not-recursive\n")
	b, _ := FreezeFileAttachment(fileRef(t, "b", NewWholeFileSelection()), "b\n")
	items := []FileAttachment{a, b}
	files, err := NewFileAttachments(items)
	if err != nil {
		t.Fatal(err)
	}
	items[0] = FileAttachment{}
	returned := files.Items()
	returned[0] = FileAttachment{}
	input, err := NewUserInput(InputID("01900000-0000-7000-8000-000000000001"), "$review @a @b")
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.WithFileAttachments(files)
	if err != nil || input.FileAttachments().Len() != 2 || input.Text() != "$review @a @b" || input.ByteSize() <= len(input.Text())+len(a.Content())+len(b.Content()) {
		t.Fatal("input lost attachments", err)
	}
	if files.Items()[0] != a || input.FileAttachments().Items()[0] != a {
		t.Fatal("shared mutable collection")
	}
	skill, err := NewSkillAttachment(SkillSource{Scope: "project", Directory: ".", Kind: "easycode"}, "review", strings.Repeat("a", 64), "@a @b", "Skill @not-recursive")
	if err != nil {
		t.Fatal(err)
	}
	combined, err := input.WithSkillAttachment(skill)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := combined.SkillAttachment(); !ok || got != skill || combined.FileAttachments().Len() != 2 || combined.ByteSize() != input.ByteSize()+skill.ByteSize() {
		t.Fatal("combined fields or budgets changed")
	}
	one, _ := NewFileAttachments([]FileAttachment{a})
	if _, err := input.WithFileAttachments(one); err == nil {
		t.Fatal("partial reference set accepted")
	}
	if _, err := NewFileAttachments([]FileAttachment{a, a}); err == nil {
		t.Fatal("duplicate reference accepted")
	}
	if _, err := DecodeFileAttachments([]byte("null")); err == nil {
		t.Fatal("null accepted")
	}
	decoded, err := DecodeFileAttachments([]byte("[]"))
	if err != nil || decoded.Len() != 0 {
		t.Fatal("empty array rejected", err)
	}
	encoded, err := codec.MarshalStable(files)
	if files.ByteSize() != len(encoded) {
		t.Fatal("file metadata budget differs from canonical bytes")
	}
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = DecodeFileAttachments(encoded)
	if err != nil || decoded.Len() != 2 {
		t.Fatal("array decode", err)
	}
	ordinary, err := NewUserInput(input.ID(), strings.Repeat("x", MaxUserInputBytes))
	if err != nil || ordinary.ByteSize() != MaxUserInputBytes {
		t.Fatal("plain input budget changed", err)
	}
}

func BenchmarkFileAttachmentLongEscapedLine(b *testing.B) {
	ref, _ := NewFileReference("long", NewWholeFileSelection())
	source := strings.Repeat("\"\\<&界", 200000)
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := FreezeFileAttachment(ref, source); err != nil {
			b.Fatal(err)
		}
	}
}

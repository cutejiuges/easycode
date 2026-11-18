package domain

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestFileSummaryWholeEncodedBudgetAndCompletePrefix(t *testing.T) {
	for _, pathSize := range []int{1, 900, 4096} {
		var attachments []FileAttachment
		for i := 0; i < MaxFileReferences; i++ {
			path := fmt.Sprintf("%d", i) + strings.Repeat("\"", pathSize-1)
			a, err := FreezeFileAttachment(fileRef(t, path, NewWholeFileSelection()), "secret-body\n")
			if err != nil {
				t.Fatal(err)
			}
			attachments = append(attachments, a)
		}
		files, err := NewFileAttachments(attachments)
		if err != nil {
			t.Fatal(err)
		}
		summary, err := NewFileSummary(files)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := codec.MarshalStable(summary)
		if err != nil || len(encoded) > MaxFileSummaryBytes || bytes.Contains(encoded, []byte("secret-body")) || summary.Total() != 8 || len(summary.Items())+summary.Omitted() != 8 {
			t.Fatal("summary bound or count", err)
		}
		for i, item := range summary.Items() {
			if item.Reference() != attachments[i].Reference() {
				t.Fatal("path truncated or reordered")
			}
		}
		if pathSize == 4096 && (len(summary.Items()) != 0 || summary.Omitted() != 8) {
			t.Fatal("overlong first item was truncated")
		}
		decoded, err := DecodeFileSummary(encoded)
		if err != nil || !decoded.Equal(summary) {
			t.Fatal("summary decode", err)
		}
		copy := summary.Items()
		if len(copy) > 0 {
			copy[0] = FileSummaryItem{}
			if summary.Items()[0].Reference() != attachments[0].Reference() {
				t.Fatal("summary getter shared items")
			}
		}
	}
}

func TestFileSummaryRejectsWrongCountsAndPrivateFields(t *testing.T) {
	for _, bad := range []string{`{}`, `null`, `{"total":0,"items":null,"omitted":0}`, `{"total":1,"items":[],"omitted":0}`, `{"total":9,"items":[],"omitted":9}`, `{"total":0,"items":[],"omitted":0,"content":"private"}`, `{"total":0,"total":0,"items":[],"omitted":0}`} {
		if _, err := DecodeFileSummary([]byte(bad)); err == nil {
			t.Fatal("invalid summary accepted", bad)
		}
	}
	a, _ := FreezeFileAttachment(fileRef(t, "a", NewWholeFileSelection()), "a\n")
	files, _ := NewFileAttachments([]FileAttachment{a})
	summary, _ := NewFileSummary(files)
	encoded, _ := codec.MarshalStable(summary)
	for _, bad := range [][]byte{
		bytes.Replace(encoded, []byte(`"path":"a"`), []byte(`"path":"a","content":"private"`), 1),
		bytes.Replace(encoded, []byte(`"rendered_bytes":`+fmt.Sprint(summary.Items()[0].RenderedBytes())), []byte(`"rendered_bytes":1`), 1),
		bytes.Replace(encoded, []byte(`"actual_start_line":1`), []byte(`"actual_start_line":2`), 1),
	} {
		if _, err := DecodeFileSummary(bad); err == nil {
			t.Fatal("invalid item accepted")
		}
	}
}

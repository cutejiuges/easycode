package tool

import (
	"strings"
	"testing"
)

func TestArtifactPagesPreserveRawProgress(t *testing.T) {
	raw := []byte("a中\xffz")
	d := testArtifactDescriptor(t, false)
	d.saved = int64(len(raw))
	d.observed = 100
	for _, tt := range []struct {
		offset, limit, next uint64
		text                string
		fail                bool
	}{
		{0, 3, 1, "a", false}, {1, 3, 4, "中", false}, {2, 3, 0, "", true}, {1, 1, 0, "", true}, {4, 2, 6, "\\xFFz", false}, {6, 4, 6, "", false}, {7, 1, 0, "", true},
	} {
		input, _ := NewReadArtifactInput(d.ID(), tt.offset, tt.limit)
		p, err := RenderArtifactRange(d, input, 0, raw)
		if (err != nil) != tt.fail {
			t.Fatalf("range %d/%d: %v", tt.offset, tt.limit, err)
		}
		if !tt.fail && (p.NextOffset() != tt.next || p.Text() != tt.text || p.SourceComplete()) {
			t.Fatalf("wrong page: %+v", p)
		}
	}
}

func TestArtifactBinaryPageBudget(t *testing.T) {
	raw := []byte(strings.Repeat("\xff", ArtifactPageBytes))
	d := testArtifactDescriptor(t, true)
	d.saved = int64(len(raw))
	d.observed = d.saved
	input, _ := NewReadArtifactInput(d.ID(), 0, ArtifactPageBytes)
	page, err := RenderArtifactRange(d, input, 0, raw)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := page.Preview()
	if err != nil || len(preview.Text()) > MaxModelPreviewBytes || page.NextOffset() == 0 || page.EOF() {
		t.Fatalf("budget: %+v %v", page, err)
	}
	if uint64(len(page.Text())) != page.NextOffset()*4 {
		t.Fatal("wrong raw offset after escaping")
	}
}

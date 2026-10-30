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
		p, err := RenderArtifactRange(d, input, 0, raw, MaxModelPreviewBytes)
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
	page, err := RenderArtifactRange(d, input, 0, raw, MaxModelPreviewBytes)
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

func TestArtifactWindowFinalizationChargesEscapesAndHeader(t *testing.T) {
	raw := []byte(strings.Repeat("中\xffa", 3000))
	d := testArtifactDescriptor(t, true)
	d.stream = ArtifactSearchResult
	d.saved = int64(len(raw))
	d.observed = d.saved
	var offset uint64
	var joined strings.Builder
	for offset < uint64(len(raw)) {
		input, _ := NewReadArtifactInput(d.ID(), offset, ArtifactPageBytes)
		start := offset - min(offset, 3)
		data := append([]byte(nil), raw[start:]...)
		window, err := NewArtifactWindow(d, input, start, data)
		if err != nil {
			t.Fatal(err)
		}
		for i := range data {
			data[i] = 'x'
		}
		page, err := window.Render(SafetyResultBytes)
		if err != nil {
			t.Fatal(err)
		}
		preview, _ := page.Preview()
		if len(preview.Text()) > SafetyResultBytes || page.NextOffset() <= offset {
			t.Fatal("unbounded or stalled page")
		}
		joined.WriteString(page.Text())
		offset = page.NextOffset()
	}
	if joined.String() != strings.Repeat("中\\xFFa", 3000) {
		t.Fatal("page boundaries lost or repeated raw bytes")
	}
	input, _ := NewReadArtifactInput(d.ID(), 0, 1)
	if _, err := NewArtifactWindow(d, input, 0, raw); err == nil {
		t.Fatal("oversized raw window accepted")
	}
}

func TestArtifactBackwardWindowsJoinWithoutGapsOrZeroProgress(t *testing.T) {
	raw := []byte(strings.Repeat("中\xffa", 40000))
	descriptor := testArtifactDescriptor(t, true)
	descriptor.saved = int64(len(raw))
	descriptor.observed = descriptor.saved
	descriptor.stream = ArtifactSearchResult
	end := uint64(len(raw))
	var pages []string
	for end > 0 {
		limit := min(end, uint64((MaxModelPreviewBytes-2048)/4))
		start := end - limit
		input, _ := NewReadArtifactInput(descriptor.ID(), start, limit)
		windowStart := start - min(start, 3)
		windowEnd := min(uint64(len(raw)), end+3)
		window, err := NewArtifactWindow(descriptor, input, windowStart, raw[windowStart:windowEnd])
		if err != nil {
			t.Fatal(err)
		}
		page, err := window.RenderBefore(end, MaxModelPreviewBytes)
		if err != nil {
			t.Fatal(err)
		}
		if page.NextOffset() != end || page.Offset() >= end || len(page.Text()) > MaxModelPreviewBytes {
			t.Fatal("backward range lost progress")
		}
		pages = append(pages, page.Text())
		end = page.Offset()
	}
	var joined strings.Builder
	for i := len(pages) - 1; i >= 0; i-- {
		joined.WriteString(pages[i])
	}
	if joined.String() != strings.Repeat("中\\xFFa", 40000) {
		t.Fatal("backward ranges lost or duplicated bytes")
	}
}

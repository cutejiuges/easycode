package tui

import (
	"github.com/rivo/uniseg"
	"strings"
	"testing"
)

func TestComposerSafeLayoutAndResize(t *testing.T) {
	text := "中e\u0301👨‍👩‍👧‍👦\t\x1b]0;title\a\x7f\u0085\u202e\u2066\n\u0301end\n"
	c := composerWithText(t, text)
	safe := safeComposerText(text)
	for _, control := range []string{"\x1b", "\a", "\x7f", "\u0085", "\u202e", "\u2066"} {
		if strings.Contains(safe, control) {
			t.Fatalf("raw control in %q", safe)
		}
	}
	if !strings.Contains(safe, "\\u001b") || !strings.Contains(safe, "👨‍👩‍👧‍👦") {
		t.Fatal(safe)
	}
	for _, size := range [][2]int{{80, 24}, {32, 10}, {4, 2}, {2, 1}, {1, 1}} {
		c.resize(size[0], size[1])
		before := c.layout
		first := c.view(true)
		second := c.view(true)
		if first != second || c.layout != before || c.text != text || c.cursor != len(text) {
			t.Fatal("render changed state")
		}
		if !strings.Contains(first, "|") {
			t.Fatal("missing cursor")
		}
		for _, line := range strings.Split(strings.TrimSuffix(first, "\n"), "\n") {
			if uniseg.StringWidth(line) > size[0] {
				t.Fatalf("width=%d line=%q", size[0], line)
			}
		}
	}
}

func TestComposerSoftWrapCursorAffinity(t *testing.T) {
	c := composerWithText(t, "abcd")
	c.resize(7, 8)
	if c.rowIndex() != 1 {
		t.Fatal("full row end must belong to next row")
	}
	c.horizontal(-1)
	if c.rowIndex() != 0 || c.cursor != 3 {
		t.Fatal("left crossed wrong boundary")
	}
	c.vertical(1)
	if c.cursor != 4 {
		t.Fatal("down missed final caret")
	}
}

func BenchmarkComposerLayout(b *testing.B) {
	for _, sample := range []struct{ name, text string }{{"ASCII", strings.Repeat("x", 4<<20)}, {"CJK", strings.Repeat("中", (4<<20)/3)}, {"Lines", strings.Repeat("code line\n", (4<<20)/10)}} {
		b.Run(sample.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				buildComposerLayout(sample.text, 77)
			}
		})
	}
}

func BenchmarkComposerEdit(b *testing.B) {
	c := newComposer()
	_ = c.replace(0, 0, strings.Repeat("x", (4<<20)-1))
	b.ReportAllocs()
	for b.Loop() {
		copy := c
		_ = copy.replace(copy.cursor, copy.cursor, "y")
	}
}

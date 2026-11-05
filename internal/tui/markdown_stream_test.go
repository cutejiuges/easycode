package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func streamText(s *markdownStream) string {
	return strings.Join(markdownLayoutLines(s.layout(80, false), "", 80), "\n")
}
func TestMarkdownStreamFinalMatchesCanonical(t *testing.T) {
	for _, source := range []string{
		"# Heading\n\nparagraph\n\n- one\n- two\n",
		"Title\n===\n\nbody\n",
		"[reference][id]\n\n[id]: https://example.invalid\n",
		"```go\npackage main\nfunc main() {}\n```\n\nend",
		"|a|b|\n|-|-|\n|one|two|\n",
		"- one\n\n  continued\n\n- two\n",
	} {
		t.Run(source, func(t *testing.T) {
			canonical := markdownTestRender(t, source, 80, false)
			for step := 1; step <= 32; step++ {
				p := newMarkdownParser()
				s := markdownStream{}
				for end := min(step, len(source)); end < len(source); end = min(end+step, len(source)) {
					s.advance(&p, source[:end], false, markdownNodeLimit)
				}
				s.advance(&p, source, true, markdownNodeLimit)
				if got := streamText(&s); got != canonical {
					t.Fatalf("step=%d\ngot=%q\nwant=%q", step, got, canonical)
				}
			}
		})
	}
}
func TestMarkdownStreamPreviewAndScanWork(t *testing.T) {
	p := newMarkdownParser()
	s := markdownStream{}
	source := strings.Repeat("a", 10000)
	for i := 1; i < len(source); i++ {
		s.advance(&p, source[:i], false, markdownNodeLimit)
	}
	if p.parses != 0 {
		t.Fatal("partial line parsed")
	}
	if len(s.preview) > 8<<10+64 {
		t.Fatal("preview exceeded bound")
	}
	s.advance(&p, source, true, markdownNodeLimit)
	if !strings.Contains(streamText(&s), strings.Repeat("a", 80)) {
		t.Fatal("final text lost")
	}
	p = newMarkdownParser()
	s = markdownStream{}
	source = ""
	for range 100 {
		source += "paragraph\n\n"
		s.advance(&p, source, false, markdownNodeLimit)
	}
	if p.scanned > len(source)*10 {
		t.Fatalf("stable prefix rescanned: %d/%d", p.scanned, len(source))
	}
}

func TestMarkdownOpenCodeHasBoundedParsingAndFinalFlush(t *testing.T) {
	p := newMarkdownParser()
	s := markdownStream{}
	source := "```go\n"
	s.advance(&p, source, false, markdownNodeLimit)
	for range 1000 {
		source += "var x = 1\n"
		s.advance(&p, source, false, markdownNodeLimit)
	}
	if p.scanned > 100 {
		t.Fatalf("open code reparsed: %d", p.scanned)
	}
	s.advance(&p, source, true, markdownNodeLimit)
	if s.codeOpen || !strings.Contains(streamText(&s), "var x = 1") {
		t.Fatal("open code did not flush")
	}
}
func TestMarkdownHighlightCacheSurvivesResize(t *testing.T) {
	p := newMarkdownParser()
	doc := p.parse("```go\npackage main\n```\n", markdownNodeLimit)
	renderMarkdown(doc, 80, true)
	before := doc.codes.scans
	renderMarkdown(doc, 34, true)
	renderMarkdown(doc, 12, false)
	renderMarkdown(doc, 12, true)
	if before != 1 || doc.codes.scans != before {
		t.Fatalf("scans=%d/%d", before, doc.codes.scans)
	}
}

func FuzzMarkdownStreamingFinal(f *testing.F) {
	for _, seed := range []string{"a\n\nb\n", "- one\n\n  continued\n\n- two\n", "```go\nx\n```\n", "<div>\nx\n</div>\n", "[x]\n\n[x]: /link\n"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 4096 || !utf8.ValidString(source) {
			return
		}
		p := newMarkdownParser()
		full := p.parse(source, markdownNodeLimit)
		if full.failure {
			return
		}
		canonical := renderMarkdown(full, 34, false)
		if canonical.failure {
			return
		}
		p = newMarkdownParser()
		stream := markdownStream{}
		for i, c := range source {
			if c == '\n' {
				prefix := source[:i+1]
				stream.advance(&p, prefix, false, markdownNodeLimit)
				oracle := newMarkdownParser()
				doc := oracle.parse(prefix, markdownNodeLimit)
				if !doc.failure {
					expected := renderMarkdown(doc, 34, false)
					actual := stream.layout(34, false)
					if !expected.failure && !actual.failure {
						want := strings.Join(markdownLayoutLines(expected, prefix, 34), "\n")
						got := strings.Join(markdownLayoutLines(actual, prefix, 34), "\n")
						if want != got {
							t.Fatalf("complete-line prefix changed: source=%q got=%q want=%q", prefix, got, want)
						}
					}
				}
			}
		}
		stream.advance(&p, source, true, markdownNodeLimit)
		layout := stream.layout(34, false)
		if layout.failure {
			return
		}
		got := strings.Join(markdownLayoutLines(layout, source, 34), "\n")
		want := strings.Join(markdownLayoutLines(canonical, source, 34), "\n")
		if got != want {
			t.Fatalf("stream changed content:\nsource=%q\ngot=%q\nwant=%q", source, got, want)
		}
	})
}

func TestMarkdownCodeSurvivesLaterBlocksAndReferenceDefinition(t *testing.T) {
	p := newMarkdownParser()
	s := markdownStream{}
	source := "```go\nfunc main() {}\n```\n"
	for i, part := range []string{"", "\n[next][id]\n", "\n[id]: /target\n"} {
		source += part
		s.advance(&p, source, false, markdownNodeLimit)
		layout := s.layout(40, true)
		if layout.failure {
			t.Fatal("layout failed")
		}
		scans := 0
		for j := 0; j <= len(s.fragments); j++ {
			doc, _ := s.document(j)
			if doc.codes != nil {
				scans += doc.codes.scans
			}
		}
		if i == 0 && scans != 1 || i > 0 && scans != 0 {
			t.Fatalf("step %d rescanned unchanged code: %d", i, scans)
		}
	}
}

func TestMarkdownTablePreviewAndNestedFenceAtChunkBoundary(t *testing.T) {
	p := newMarkdownParser()
	s := markdownStream{}
	s.advance(&p, "|left|right|\n|---|---|\n|half", false, markdownNodeLimit)
	if strings.Contains(streamText(&s), "half") {
		t.Fatal("partial table row shown")
	}
	s = markdownStream{}
	source := "> ```go\n> package main\n"
	s.advance(&p, source, false, markdownNodeLimit)
	s.layout(40, true)
	if s.tail.codes.scans != 0 {
		t.Fatal("open quote fence scanned")
	}
	s.advance(&p, source, true, markdownNodeLimit)
	s.layout(40, true)
	if s.tail.codes.scans != 1 {
		t.Fatal("nested fence not highlighted at completion")
	}
}

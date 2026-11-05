package tui

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func markdownTestRender(t *testing.T, source string, width int, color bool) string {
	t.Helper()
	p := newMarkdownParser()
	doc := p.parse(source, markdownNodeLimit)
	if doc.failure {
		t.Fatal("parse budget unexpectedly rejected fixture")
	}
	layout := renderMarkdown(doc, width, color)
	if layout.failure {
		t.Fatal("layout failed")
	}
	return strings.Join(markdownLayoutLines(layout, source, width), "\n")
}
func TestMarkdownStructuredContent(t *testing.T) {
	source := "# Heading\n\n- [x] done\n- **bold** and *italic*\n\n> quote\n\n[docs](https://example.invalid) ![alt](image.png)\n\n```go\npackage main\n\nfunc main() {}\n```\n"
	rendered := markdownTestRender(t, source, 80, false)
	for _, expected := range []string{"Heading", "[x] done", "bold and italic", "│ quote", "docs (https://example.invalid)", "alt (image.png)", "package main", "func main() {}"} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("missing %q:\n%s", expected, rendered)
		}
	}
	if strings.Contains(rendered, "**") {
		t.Fatal("emphasis markers leaked")
	}
	color := markdownTestRender(t, source, 80, true)
	if ansi.Strip(color) != rendered {
		t.Fatalf("color changed content:\n%s", color)
	}
}
func TestMarkdownNarrowTableAndSafety(t *testing.T) {
	source := "| name | path |\n|---|---|\n| 中文 | /a/very/long/path/that/needs/wrapping |\n"
	narrow := markdownTestRender(t, source, 12, false)
	if !strings.Contains(narrow, "name: 中文") {
		t.Fatalf("missing vertical table: %s", narrow)
	}
	for _, line := range strings.Split(narrow, "\n") {
		if ansi.StringWidth(line) > 12 {
			t.Fatalf("overflow: %q", line)
		}
	}
	for _, source := range []string{"&#27;]52;c;bad&#7;", "[x](https://x.invalid/&#27;)", "<b>html</b>", "👩‍💻 é \u202e bad"} {
		rendered := markdownTestRender(t, source, 34, false)
		if strings.ContainsAny(rendered, "\x1b\a\u202e") {
			t.Fatalf("terminal injection: %q", rendered)
		}
	}
}
func TestMarkdownParserInstancesAndReservation(t *testing.T) {
	a, b := newMarkdownParser(), newMarkdownParser()
	a.parse("[x]: /one\n\n[x]", markdownNodeLimit)
	doc := b.parse("[x]", markdownNodeLimit)
	rendered := strings.Join(markdownLayoutLines(renderMarkdown(doc, 80, false), "[x]", 80), "\n")
	if rendered != "[x]" {
		t.Fatalf("reference leaked: %s", rendered)
	}
	before := b.parses
	if !b.parse(strings.Repeat("|", 20000), markdownNodeLimit).failure || b.parses != before {
		t.Fatal("budget checked after parse")
	}
}
func TestMarkdownViewDoesNotParse(t *testing.T) {
	model := snapshotCompletedModel(t)
	before := model.markdown.parser.parses
	for range 100 {
		model.View()
	}
	if model.markdown.parser.parses != before {
		t.Fatal("View parsed source")
	}
}

func TestMarkdownBudgetFailureIsStableAndVisible(t *testing.T) {
	cache := newMarkdownViewCache()
	source := strings.Repeat("|", 20000)
	key := markdownViewKey{local: 1}
	initial := strings.Join(cache.prepare(key, source, 80, true), "\n")
	if !strings.Contains(initial, "Markdown unavailable") {
		t.Fatal("budget reason missing")
	}
	parses := cache.parser.parses
	for range 100 {
		if got := strings.Join(cache.prepare(key, source, 80, true), "\n"); got != initial {
			t.Fatal("failed source disappeared on repaint")
		}
	}
	if cache.parser.parses != parses {
		t.Fatal("budget failure retried on repaint")
	}
	if !strings.Contains(strings.Join(cache.prepare(key, source, 34, true), "\n"), "Markdown unavailable") {
		t.Fatal("resize lost failure explanation")
	}
}

func TestMarkdownEntitiesPreserveGraphemeAcrossRuns(t *testing.T) {
	for _, source := range []string{"e&#x301;", "👩&#x200d;💻", "**e**&#x301;"} {
		text := markdownTestRender(t, source, 34, false)
		if strings.Contains(text, "◌") {
			t.Fatalf("grapheme split at entity/style boundary: %q", text)
		}
		if ansi.Strip(markdownTestRender(t, source, 34, true)) != text {
			t.Fatal("style changed grapheme")
		}
	}
}

func TestMarkdownTypicalLargeCodeRemainsHighlighted(t *testing.T) {
	source := "```go\n" + strings.Repeat("var value = 123\n", 4000) + "```\n"
	cache := newMarkdownViewCache()
	cache.color = true
	lines := cache.prepare(markdownViewKey{local: 1}, source, 78, true)
	rendered := strings.Join(lines, "\n")
	if strings.Contains(rendered, "unavailable") || !strings.Contains(rendered, "\x1b[") {
		t.Fatal("typical large code fell back instead of highlighting")
	}
	if cost := cache.cost(); cost.nodes > markdownNodeLimit || cost.bytes > markdownMemoryLimit {
		t.Fatalf("cache exceeded budget: %+v", cost)
	}
}

func TestMarkdownTableEmptyAndDuplicateHeaders(t *testing.T) {
	source := "| same | same | |\n|---|---|---|\n| one | two | three |\n"
	rendered := markdownTestRender(t, source, 12, false)
	for _, value := range []string{"Column 1: one", "Column 2: two", "Column 3: three"} {
		if !strings.Contains(strings.ReplaceAll(rendered, "\n", ""), value) {
			t.Fatalf("missing %q: %s", value, rendered)
		}
	}
	source = "| name | value |\n|---|---|\n| x | " + strings.Repeat("long", 40) + " |\n"
	if !strings.Contains(markdownTestRender(t, source, 34, false), "name: x") {
		t.Fatal("tall record did not use vertical layout")
	}
}
func TestMarkdownControlSourceMapping(t *testing.T) {
	p := newMarkdownParser()
	source := "before\x1b\n\nafter\n"
	doc := p.parse(source, markdownNodeLimit)
	if strings.ContainsRune(string(doc.source), '\x1b') {
		t.Fatal("control reached parser")
	}
	offset := strings.Index(string(doc.source), "after")
	if doc.originalOffset(offset) != strings.Index(source, "after") {
		t.Fatal("safe display changed original source offset")
	}
}

func TestMarkdownModelBudgetAtNarrowWidth(t *testing.T) {
	for _, source := range []string{strings.Repeat("x", 65536), strings.Repeat("中", 21845), strings.Repeat("|ab|cd|\n", 8000), strings.Repeat("> ", 8000) + "end"} {
		model := NewModel(context.Background(), "test", nil)
		for i := 1; i <= 4; i++ {
			model.transcript = append(model.transcript, transcriptMessage{local: uint64(i), role: "Assistant", text: source, excerpt: i == 4})
		}
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 4, Height: 12})
		cost := model.markdown.cost()
		t.Logf("source_bytes=%d first=%q retained_bytes=%d nodes=%d", len(source), source[:6], cost.bytes, cost.nodes)
		if cost.bytes > markdownMemoryLimit || cost.nodes > markdownNodeLimit {
			t.Fatalf("model exceeds derived budget: source=%q cost=%+v", source[:8], cost)
		}
	}
}

// 合并时两份输入行数组及新数组必须同时纳入预算。
func TestMarkdownMergePeakBudget(t *testing.T) {
	makePart := func() markdownLayout { return markdownLayout{rows: []markdownRow{{text: "x"}}, bytes: 25} }
	for _, limit := range []int{97, 98, 99} {
		left := makePart()
		left.merge(makePart(), 7, limit)
		if limit < 98 {
			if !left.failure || len(left.rows) != 0 {
				t.Fatal("partial over-budget rows published")
			}
			continue
		}
		if left.failure || left.rows[1].source != 7 || left.bytes != 50 {
			t.Fatalf("incorrect merged layout: %+v", left)
		}
	}
}

func TestMarkdownFiftyCollapsedBodiesShareLayoutBudget(t *testing.T) {
	model := NewModel(context.Background(), "test", nil)
	for i := 1; i <= 50; i++ {
		model.transcript = append(model.transcript, transcriptMessage{local: uint64(i), role: "Assistant", text: strings.Repeat("x", 65536)})
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 3, Height: 12})
	cost := model.markdown.cost()
	t.Logf("50 items, width=3, retained_bytes=%d nodes=%d", cost.bytes, cost.nodes)
	if cost.bytes > markdownMemoryLimit {
		t.Fatalf("combined rows and anchors: %d bytes", cost.bytes)
	}
}

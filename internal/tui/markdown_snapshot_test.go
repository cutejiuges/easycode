package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownFixedWindowSnapshots(t *testing.T) {
	source := "# Plan\n\n- [x] inspect\n- [ ] implement\n\n```go\nfunc main() {\n    println(\"你好\")\n}\n```\n\n| name | location |\n|---|---|\n| 中文 | /workspace/a/very/long/path |\n"
	for _, size := range []struct{ width, height int }{{80, 24}, {34, 12}, {12, 6}} {
		for _, color := range []bool{false, true} {
			name := fmt.Sprintf("markdown_%dx%d_color%t", size.width, size.height, color)
			t.Run(name, func(t *testing.T) {
				model := NewModel(context.Background(), "test", nil).WithMarkdownColor(color)
				model.transcript = []transcriptMessage{{role: "Assistant", text: source}}
				model = updateModel(t, model, tea.WindowSizeMsg{Width: size.width, Height: size.height})
				got := model.View()
				checkMarkdownGolden(t, name, got)
				if strings.Contains(got, "```go") {
					t.Fatal("complete Markdown fence leaked")
				}
			})
		}
	}
}

func checkMarkdownGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv("EASYCODE_UPDATE_MARKDOWN_SNAPSHOTS") == "1" {
		if err := os.WriteFile(path, []byte(got), 0600); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(expected) {
		t.Fatalf("snapshot mismatch\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

func TestMarkdownInteractionSnapshots(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 34, Height: 12}, {Width: 12, Height: 6}} {
		for _, color := range []bool{false, true} {
			name := fmt.Sprintf("markdown_reading_%dx%d_color%t", size.Width, size.Height, color)
			t.Run(name, func(t *testing.T) {
				turn := domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdef")
				q := &mainTestQueries{page: protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{{Cursor: protocol.TranscriptCursor{Sequence: 7}, Kind: domain.TranscriptAssistant, TurnID: turn, Text: "# Restored\n\nStable **body**"}}}}
				model := NewModel(context.Background(), "test", nil).WithTranscriptQueries(domain.ThreadID(turn), q).WithMarkdownColor(color)
				model.state = stateStreaming
				model.transcript = []transcriptMessage{{role: "Assistant", local: 1, text: "Opening [reference](https://private.invalid"}}
				model = updateModel(t, model, size)
				var seen strings.Builder
				capture := func(label string) { seen.WriteString(label + "\n" + model.View() + "\n") }
				capture("stream")
				model.transcript[0].text = "## Review\n\n" + strings.Repeat("next paragraph\n\n", 20)
				model.prepareMarkdownView()
				model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
				capture("reading")
				model.transcript[0].text += "New output\n"
				model.prepareMarkdownView()
				capture("new output")
				for model.reading.detached {
					model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
				}
				next, query := model.commitMainTranscript(7)
				model = next.(Model)
				model.prepareMarkdownView()
				capture("handoff")
				next, _ = model.Update(query())
				model = next.(Model)
				capture("committed")
				checkMarkdownGolden(t, name, seen.String())
			})
		}
	}
}

// 同一正文覆盖六种语言和真实深度回退，固定终端下对照有色与无色结果。
func TestMarkdownHighlightWindowSnapshots(t *testing.T) {
	var source strings.Builder
	for _, sample := range []struct{ lang, code string }{
		{"go", "var n = 42"}, {"json", `{"ok":true}`}, {"py", "return f'{n}'"}, {"js", "const x = /a/g;"}, {"ts", "type Count = number;"}, {"sh", "echo \"$HOME\""},
		{"js", "`x${" + strings.Repeat("(", 65) + "v" + strings.Repeat(")", 65) + "}`"},
	} {
		fmt.Fprintf(&source, "```%s\n%s\n```\n", sample.lang, sample.code)
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 34, Height: 12}, {Width: 12, Height: 6}} {
		for _, color := range []bool{false, true} {
			name := fmt.Sprintf("markdown_highlight_%dx%d_color%t", size.Width, size.Height, color)
			model := NewModel(context.Background(), "test", nil).WithMarkdownColor(color)
			model.transcript = []transcriptMessage{{role: "Assistant", text: source.String()}}
			model = updateModel(t, model, size)
			view := model.View()
			if size.Width == 80 && strings.Contains(view, "Highlighting unavailable") != color {
				t.Fatal("unexpected highlight fallback")
			}
			checkMarkdownGolden(t, name, view)
		}
	}
}

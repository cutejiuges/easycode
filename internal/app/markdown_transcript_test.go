package app

import (
	"bytes"
	"easycode/internal/domain"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarkdownBothProvidersRestoreAndContinueUnchanged(t *testing.T) {
	runProvidersPersistResumeReplayAndContinue(t, func(value string) string { return "# " + value + "\n\n```go\nvar value = 42\n```\n" }, func(t *testing.T, model tea.Model, root string) {
		before := markdownJournalBytes(t, root)
		apply := func(message tea.Msg) {
			var cmd tea.Cmd
			model, cmd = model.Update(message)
			model = drainMarkdownCommands(t, model, cmd)
			if strings.Contains(model.View(), "private-opaque-") {
				t.Fatal("opaque reasoning reached TUI")
			}
		}
		for _, size := range []tea.WindowSizeMsg{{Width: 34, Height: 12}, {Width: 80, Height: 24}} {
			apply(size)
		}
		apply(tea.KeyMsg{Type: tea.KeyCtrlO})
		apply(tea.KeyMsg{Type: tea.KeyEnter})
		if !strings.Contains(model.View(), "var value = 42") {
			t.Fatalf("rich detail missing code: %s", model.View())
		}
		apply(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
		if !strings.Contains(model.View(), "```go") {
			t.Fatalf("raw detail missing fence: %s", model.View())
		}
		apply(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
		apply(tea.KeyMsg{Type: tea.KeyEsc})
		apply(tea.KeyMsg{Type: tea.KeyCtrlO})
		after := markdownJournalBytes(t, root)
		if len(before) != len(after) {
			t.Fatal("browsing changed journal set")
		}
		for path, contents := range before {
			if !bytes.Equal(contents, after[path]) {
				t.Fatalf("browsing changed journal %s", path)
			}
		}
	})
}
func drainMarkdownCommands(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	if cmd == nil {
		return model
	}
	message := cmd()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, nested := range batch {
			model = drainMarkdownCommands(t, model, nested)
		}
		return model
	}
	next, follow := model.Update(message)
	return drainMarkdownCommands(t, next, follow)
}
func markdownJournalBytes(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".jsonl") {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] = data
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) == 0 {
		t.Fatal("no journal captured")
	}
	return result
}

func addMarkdownReasoningFixture(wire []byte, family domain.ProviderFamily, turn int) []byte {
	ending := `data: {"type":"message_stop"`
	frames := fmt.Sprintf("data: %s\n\ndata: %s\n\n", fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"private-opaque-%d"}}`, turn), `{"type":"content_block_stop","index":1}`)
	if family == domain.ProviderOpenAI {
		ending = `data: {"type":"response.completed"`
		frames = fmt.Sprintf("data: %s\n\n", fmt.Sprintf(`{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs-%d","summary":[],"encrypted_content":"private-opaque-%d"}}`, turn, turn))
	}
	return bytes.Replace(wire, []byte(ending), []byte(frames+ending), 1)
}
func assertMarkdownHistoryOrder(t *testing.T, body []byte) {
	t.Helper()
	for _, value := range []string{"first", "answer-1", "private-opaque-1", "second", "answer-2", "private-opaque-2", "third"} {
		marker := []byte(fmt.Sprintf("%q", value))
		index := bytes.Index(body, marker)
		if index < 0 {
			t.Fatalf("missing native content or wrong order at %s", value)
		}
		body = body[index+len(marker):]
	}
}

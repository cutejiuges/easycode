package tui

import (
	"reflect"
	"strings"
	"testing"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func TestCompletionSeparatorPreservesGraphemeCursor(t *testing.T) {
	for _, separator := range []string{" \u0301", "\u3000\u0301", " \ufe0f", " \u200d", "\t", "\n", "\u2003"} {
		t.Run(separator, func(t *testing.T) {
			m := completionModel(t, "$rev"+separator+"tail")
			m.composer.moveTo(len("$rev"))
			m.prepareCompletion()
			m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyTab})
			if cmd != nil || m.composer.text != "$review"+separator+"tail" {
				t.Fatal("completion changed the argument suffix")
			}
			graphemes := uniseg.NewGraphemes(separator + "tail")
			if !graphemes.Next() {
				t.Fatal("missing separator grapheme")
			}
			wantCursor := len("$review") + len(graphemes.Str())
			if m.composer.cursor != wantCursor || int(m.composer.layout.points[m.composer.pointIndex()].offset) != m.composer.cursor {
				t.Fatalf("cursor is inside a grapheme: got %d, want %d", m.composer.cursor, wantCursor)
			}
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
			want := "$review" + strings.TrimPrefix(separator+"tail", graphemes.Str())
			if m.composer.text != want {
				t.Fatalf("backspace split the separator grapheme: %q, want %q", m.composer.text, want)
			}
		})
	}
}

func TestCompletionEmptyPasteIsNoOp(t *testing.T) {
	m := completionModel(t, "$rev")
	before := m
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter, Paste: true})
	if cmd != nil || !reflect.DeepEqual(m.composer, before.composer) || !reflect.DeepEqual(m.completion, before.completion) {
		t.Fatal("empty paste changed completion, draft or submitted input")
	}
}

func TestCompletionRejectedAdmissionKeepsCurrentWindow(t *testing.T) {
	for _, kind := range []string{"input", "context", "cancel-context"} {
		t.Run(kind, func(t *testing.T) {
			m := completionModel(t, "$rev "+strings.Repeat("argument ", 12))
			m.composer.moveTo(4)
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
			before := m.composer
			var command protocol.Command
			var err error
			if kind == "input" {
				command, err = protocol.NewSubmitConversationalInputCommand("revalidation", m.composer.text)
			} else {
				command, err = protocol.NewQueryContextCommand("revalidation")
				m.pendingContext = protocol.CommandQueryContext
			}
			if err != nil {
				t.Fatal(err)
			}
			m.pending = &before
			m.state = stateSubmitting
			m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
			if kind == "cancel-context" {
				m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			} else {
				result, err := protocol.NewRejectedCommandResult(command, protocol.ControlErrorSessionBusy)
				if err != nil {
					t.Fatal(err)
				}
				item, err := protocol.NewCommandResultItem(result)
				if err != nil {
					t.Fatal(err)
				}
				m = updateModel(t, m, item)
			}
			if m.composer.width != 28 || m.composer.height != 10 || m.composer.text != before.text || m.composer.cursor != before.cursor {
				t.Fatalf("restored stale window: %dx%d cursor=%d", m.composer.width, m.composer.height, m.composer.cursor)
			}
			for _, line := range strings.Split(m.composer.view(true), "\n") {
				if ansi.StringWidth(line) > 28 {
					t.Fatal("restored draft overflows current terminal", line)
				}
			}
		})
	}
}

func TestCompletionExtremelyNarrowWindowCannotAcceptBlindSelection(t *testing.T) {
	for _, width := range []int{1, 2, 4, 8, 12} {
		m := completionModel(t, "/con")
		before := m.composer.text
		m = updateModel(t, m, tea.WindowSizeMsg{Width: width, Height: 24})
		if m.completion.rows != 0 {
			t.Fatalf("width %d leaves an unreadable candidate active: %q", width, m.completion.view)
		}
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyTab})
		if cmd != nil || m.composer.text != before {
			t.Fatal("Tab accepted an unreadable choice")
		}
		m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		if m.completion.rows == 0 || m.composer.text != before {
			t.Fatal("normal window did not restore the unchanged candidate")
		}
	}
}

func TestCompletionRestoredSnapshotInvalidatesEditingCache(t *testing.T) {
	m := completionModel(t, "$rev")
	other := composerWithText(t, "$zzz")
	if m.composer.revision != other.revision || m.composer.cursor != other.cursor {
		t.Fatal("fixture does not exercise equal edit serials")
	}
	m.restoreComposer(other)
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.composer.text != "$zzz" || m.completion.rows != 0 {
		t.Fatal("restored draft reused a different snapshot's matches")
	}
}

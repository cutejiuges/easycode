package tui

import (
	"context"
	"io"
	"testing"
	"time"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type composerObservation struct {
	text  string
	state turnState
	paste bool
}
type observedComposer struct {
	model    Model
	observed chan<- composerObservation
}

func (m observedComposer) Init() tea.Cmd { return m.model.Init() }
func (m observedComposer) View() string  { return m.model.View() }
func (m observedComposer) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.model.Update(message)
	m.model = updated.(Model)
	switch value := message.(type) {
	case tea.KeyMsg:
		m.observed <- composerObservation{m.model.composer.text, m.model.state, value.Paste}
	case RuntimeEventMsg:
		if value.Event.Kind == protocol.EventTurnCompleted {
			m.observed <- composerObservation{m.model.composer.text, m.model.state, false}
		}
	}
	return m, cmd
}

func TestComposerTerminalDecodesPasteAndNewlineKeys(t *testing.T) {
	for _, split := range []bool{false, true} {
		name := "whole"
		if split {
			name = "fragmented"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			input, writer := io.Pipe()
			observed := make(chan composerObservation, 16)
			session := &fakeChatSession{events: make(chan protocol.Event, 1)}
			session.events <- mustRuntimeCompleted(t)
			model := NewModel(ctx, "test", session)
			program := tea.NewProgram(observedComposer{model, observed}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
			done := make(chan error, 1)
			go func() { _, err := program.Run(); done <- err }()
			t.Cleanup(func() {
				program.Kill()
				writer.Close()
				input.Close()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("program cleanup timed out")
				}
			})
			write := func(text string) {
				t.Helper()
				if _, err := io.WriteString(writer, text); err != nil {
					t.Fatal(err)
				}
			}
			receive := func() composerObservation {
				t.Helper()
				select {
				case result := <-observed:
					return result
				case <-ctx.Done():
					t.Fatal("missing decoded input")
					return composerObservation{}
				}
			}
			if split {
				for _, part := range []string{"\x1b[200~", "  中文\r", "\n\tcode  ", "\x1b[201~"} {
					write(part)
				}
			} else {
				write("\x1b[200~  中文\r\n\tcode  \x1b[201~")
			}
			pasted := receive()
			if !pasted.paste || pasted.text != "  中文\n\tcode  " || pasted.state != stateIdle {
				t.Fatalf("paste=%#v", pasted)
			}
			write("\x1b\r")
			if got := receive(); got.text != "  中文\n\tcode  \n" || got.state != stateIdle {
				t.Fatalf("Alt+Enter=%#v", got)
			}
			write("\n")
			if got := receive(); got.text != "  中文\n\tcode  \n\n" || got.state != stateIdle {
				t.Fatalf("Ctrl+J=%#v", got)
			}
			write("\r")
			if got := receive(); got.state != stateSubmitting {
				t.Fatalf("submit=%#v", got)
			}
			if got := receive(); got.state != stateIdle || got.text != "" {
				t.Fatalf("terminal=%#v", got)
			}
			session.mu.Lock()
			submits := append([]string(nil), session.submits...)
			session.mu.Unlock()
			if len(submits) != 1 || submits[0] != "  中文\n\tcode  \n\n" {
				t.Fatalf("submits=%q", submits)
			}
			write("\x03")
			receive()
		})
	}
}

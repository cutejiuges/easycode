package tui

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type completionObservation struct {
	text  string
	rows  int
	state turnState
}
type completionObserver struct {
	model    Model
	observed chan<- completionObservation
}

func (m completionObserver) Init() tea.Cmd { return m.model.Init() }
func (m completionObserver) View() string  { return m.model.View() }
func (m completionObserver) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(msg)
	m.model = next.(Model)
	switch msg.(type) {
	case tea.KeyMsg, protocol.ControlItem:
		m.observed <- completionObservation{m.model.composer.text, m.model.completion.rows, m.model.state}
	}
	return m, cmd
}

func TestCompletionTerminalDecodedKeysAndFragmentedPaste(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(map[bool]string{false: "whole", true: "fragmented"}[split], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			observed := make(chan completionObservation, 64)
			s := &fakeChatSession{events: make(chan protocol.Event, 1)}
			s.events <- mustRuntimeCompleted(t)
			m := completionModel(t, "")
			m.session = s
			m.outputs = s.Outputs()
			program := tea.NewProgram(completionObserver{m, observed}, tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
			done := make(chan error, 1)
			go func() { _, err := program.Run(); done <- err }()
			t.Cleanup(func() {
				program.Kill()
				writer.Close()
				input.Close()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("terminal owner did not finish")
				}
			})
			write := func(text string) {
				t.Helper()
				if _, err := io.WriteString(writer, text); err != nil {
					t.Fatal(err)
				}
			}
			await := func(text string, visible bool, state turnState) {
				t.Helper()
				for {
					select {
					case got := <-observed:
						if got.text == text && (got.rows > 0) == visible && got.state == state {
							return
						}
					case <-ctx.Done():
						t.Fatalf("missing terminal state %q visible=%v state=%v", text, visible, state)
					}
				}
			}
			write("$rev")
			await("$rev", true, stateIdle)
			write("\x1b")
			await("$rev", false, stateIdle)
			write("\t")
			await("$rev", true, stateIdle)
			write("\r")
			await("$review ", false, stateIdle)
			if split {
				for _, part := range []string{"\x1b[200~", "args\r", "\n\t\x1b\x03", "\x1b[201~"} {
					write(part)
				}
			} else {
				write("\x1b[200~args\r\n\t\x1b\x03\x1b[201~")
			}
			text := "$review args\n\t\x1b\x03"
			await(text, false, stateIdle)
			write("\x1b\r")
			text += "\n"
			await(text, false, stateIdle)
			write("\n")
			text += "\n"
			await(text, false, stateIdle)
			write("\r")
			await(text, false, stateSubmitting)
			await("", false, stateIdle)
			s.mu.Lock()
			submits := append([]string(nil), s.submits...)
			s.mu.Unlock()
			if !reflect.DeepEqual(submits, []string{text}) {
				t.Fatal("decoded controls changed input", submits)
			}
			write("\x03")
		})
	}
}

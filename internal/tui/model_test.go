package tui

import (
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/fault"
	"easycode/internal/protocol"
)

type fakeChatSession struct {
	mu         sync.Mutex
	submits    []string
	events     chan protocol.Event
	submitErr  error
	interrupts int
}

func (session *fakeChatSession) Submit(text string) (<-chan protocol.Event, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.submits = append(session.submits, text)
	if session.submitErr != nil {
		return nil, session.submitErr
	}
	if session.events == nil {
		session.events = make(chan protocol.Event, 8)
	}
	return session.events, nil
}

func (session *fakeChatSession) Interrupt() {
	session.mu.Lock()
	session.interrupts++
	session.mu.Unlock()
}

func TestModelSubmitsAndProjectsOrderedDeltas(t *testing.T) {
	session := &fakeChatSession{}
	model := NewModel("test", session)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command == nil || model.state != stateStreaming || len(model.transcript) != 1 {
		t.Fatalf("submit state: %#v", model)
	}
	ready := command()
	model, command = updateModelWithCommand(t, model, ready)
	if command == nil {
		t.Fatal("missing event wait command")
	}

	first, _ := protocol.NewAssistantTextDelta("hel")
	second, _ := protocol.NewAssistantTextDelta("lo")
	session.events <- first
	model, command = updateModelWithCommand(t, model, command())
	session.events <- second
	model, command = updateModelWithCommand(t, model, command())
	session.events <- protocol.NewEvent(protocol.EventTurnCompleted)
	model, command = updateModelWithCommand(t, model, command())

	if command != nil || model.state != stateIdle {
		t.Fatalf("completed state: %#v command=%v", model, command)
	}
	if len(model.transcript) != 2 || model.transcript[1].text != "hello" {
		t.Fatalf("transcript: %#v", model.transcript)
	}
	if len(session.submits) != 1 || session.submits[0] != "hello" {
		t.Fatalf("submits: %#v", session.submits)
	}
}

func TestModelInputAndCancellationKeys(t *testing.T) {
	session := &fakeChatSession{}
	model := NewModel("test", session)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if string(model.draft) != "q" {
		t.Fatalf("q is not draft input: %q", string(model.draft))
	}

	empty := NewModel("test", session)
	updated, command := empty.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || len(updated.(Model).transcript) != 0 {
		t.Fatal("empty input was submitted")
	}

	model.state = stateStreaming
	model.events = make(chan protocol.Event)
	before := len(session.submits)
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || len(session.submits) != before || updated.(Model).state != stateStreaming {
		t.Fatal("streaming enter started another turn")
	}
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command == nil {
		t.Fatal("escape did not create interrupt command")
	}
	_ = command()
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = command()
	if session.interrupts != 2 {
		t.Fatalf("interrupt count: %d", session.interrupts)
	}

	idle := NewModel("test", session)
	_, command = idle.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if command == nil {
		t.Fatal("idle empty Ctrl+C did not quit")
	}
	if message := command(); message != tea.Quit() {
		t.Fatalf("quit message: %#v", message)
	}
}

func TestModelQueuesInterruptUntilSubmitIsReady(t *testing.T) {
	session := &fakeChatSession{events: make(chan protocol.Event, 1)}
	session.events <- protocol.NewEvent(protocol.EventTurnStarted)
	model := NewModel("test", session)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	updated, submitCommand := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)

	updated, interruptCommand := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if interruptCommand != nil || !model.interruptNext {
		t.Fatalf("interrupt was not queued: %#v", model)
	}

	model, interruptCommand = updateModelWithCommand(t, model, submitCommand())
	batch, ok := interruptCommand().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("queued commands: %#v", batch)
	}
	for _, command := range batch {
		_ = command()
	}
	if session.interrupts != 1 || model.interruptNext {
		t.Fatalf("queued interrupt result: interrupts=%d model=%#v", session.interrupts, model)
	}
}

func TestModelFailureAndCancellationRemainRecoverable(t *testing.T) {
	for _, test := range []struct {
		name      string
		code      fault.Code
		cancelled bool
	}{
		{name: "failed", code: fault.CodeProviderRequest},
		{name: "cancelled", code: fault.CodeUserCancelled, cancelled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &fakeChatSession{}
			model := NewModel("test", session)
			model.transcript = []transcriptMessage{{role: "User", text: "old"}, {role: "Assistant", text: "kept"}}
			model.state = stateStreaming
			model.assistantItem = 1
			failure, err := protocol.NewTurnFailed(string(test.code), "safe failure", test.cancelled)
			if err != nil {
				t.Fatalf("new failure: %v", err)
			}
			model = updateModel(t, model, RuntimeEventMsg{Event: failure})
			if model.state != stateIdle || model.transcript[1].text != "kept" || !strings.Contains(model.errorSummary, string(test.code)) {
				t.Fatalf("failure state: %#v", model)
			}
			model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("retry")})
			updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if command == nil || updated.(Model).state != stateStreaming {
				t.Fatal("model did not recover for retry")
			}
		})
	}
}

func TestModelSubmitErrorIsSanitized(t *testing.T) {
	session := &fakeChatSession{submitErr: fault.New(fault.CodeProviderRequest, "provider request failed")}
	model := NewModel("test", session)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	model = updateModel(t, model, command())
	if model.errorSummary != "provider_request_failed: provider request failed" || model.state != stateIdle {
		t.Fatalf("submit error: %#v", model)
	}
}

func updateModel(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(Model)
}

func updateModelWithCommand(t *testing.T, model Model, message tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, command := model.Update(message)
	return updated.(Model), command
}

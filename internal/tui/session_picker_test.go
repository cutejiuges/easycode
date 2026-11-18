package tui

import (
	"context"
	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"
)

type pickerTestHost struct {
	requests           []PickerRequest
	results            chan PickerResult
	cancelled, stopped bool
	accept             bool
}

func (h *pickerTestHost) Submit(r PickerRequest) bool {
	h.requests = append(h.requests, r)
	return true
}
func (h *pickerTestHost) Results() <-chan PickerResult { return h.results }
func (h *pickerTestHost) CancelPending()               { h.cancelled = true }
func (h *pickerTestHost) Stop()                        { h.stopped = true }
func (h *pickerTestHost) Accept(uint64) bool           { return h.accept }
func pickerTestModel(t *testing.T) (StartupModel, *pickerTestHost) {
	t.Helper()
	h := &pickerTestHost{results: make(chan PickerResult, 10)}
	m := NewStartupModel(h, PickerConfig{Family: domain.ProviderOpenAI, Wire: "responses", Model: "gpt", CWD: "/here"})
	m = pickerUpdate(m, pickerStart{})
	request := m.request
	m = pickerUpdate(m, PickerResult{Request: request, Entries: []SessionEntry{
		{ThreadID: "01900000-0000-7000-8000-000000000001", Family: domain.ProviderOpenAI, Wire: "responses", Model: "gpt", CWD: "/here", UpdatedAt: time.Unix(0, 0)},
		{ThreadID: "01900000-0000-7000-8000-000000000002", Family: domain.ProviderAnthropic, Wire: "messages", Model: "Claude", CWD: "/else"},
	}})
	return m, h
}
func pickerUpdate(m StartupModel, msg tea.Msg) StartupModel {
	next, _ := m.Update(msg)
	return next.(StartupModel)
}
func pickerKey(m StartupModel, key tea.KeyType) StartupModel {
	return pickerUpdate(m, tea.KeyMsg{Type: key})
}

func TestPickerResumingDoesNotSubmitBufferedInputToChat(t *testing.T) {
	m, h := pickerTestModel(t)
	m = pickerKey(m, tea.KeyEnter)
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("do not submit")})
	m = pickerKey(m, tea.KeyEnter)
	session := &fakeChatSession{}
	chat := NewModel(context.Background(), "test", session)
	h.accept = true
	m = pickerUpdate(m, PickerResult{Request: m.request, Chat: &chat})
	if m.chat == nil || strings.Contains(m.View(), "do not submit") || len(session.submits) != 0 {
		t.Fatal("resuming input crossed into chat")
	}
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new draft")})
	if !strings.Contains(m.View(), "new draft") {
		t.Fatal("existing chat composer not delegated")
	}
}

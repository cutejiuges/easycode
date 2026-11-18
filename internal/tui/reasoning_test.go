package tui

import (
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func reasoningModel(t *testing.T) Model {
	t.Helper()
	m, _, _ := browserModel(t)
	m.activeTurnID = domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdea")
	m.reasoning = activeReasoning{}
	return m
}
func reasoningUIEvent(t *testing.T, m Model, event protocol.Event) Model {
	t.Helper()
	event.TurnID = m.activeTurnID
	updated, cmd := m.projectRuntimeEvent(event)
	if cmd != nil {
		t.Fatal("reasoning delta created asynchronous work")
	}
	m = updated.(Model)
	m.prepareMarkdownView()
	m.prepareMarkdownDetail()
	if strings.Contains(m.errorSummary, "stream_protocol_error") {
		t.Fatal(m.errorSummary)
	}
	return m
}
func reasoningUIStart(t *testing.T, m Model, block uint32) Model {
	event, _ := protocol.NewReasoningStarted(block, domain.ReasoningOpenAISummary)
	return reasoningUIEvent(t, m, event)
}
func reasoningUIDelta(t *testing.T, m Model, block uint32, offset uint64, text string) Model {
	event, err := protocol.NewReasoningDelta(block, domain.ReasoningOpenAISummary, offset, text)
	if err != nil {
		t.Fatal(err)
	}
	return reasoningUIEvent(t, m, event)
}
func reasoningUIComplete(t *testing.T, m Model, block uint32, text string) Model {
	availability := domain.ReasoningReadable
	if text == "" {
		availability = domain.ReasoningUnavailable
	}
	event, err := protocol.NewReasoningCompleted(block, domain.ReasoningOpenAISummary, protocol.ReasoningCompletion{Availability: availability, TotalBytes: uint64(len(text)), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return reasoningUIEvent(t, m, event)
}

func TestReasoningReaderCancellationRemainsAboveLateBoundaries(t *testing.T) {
	m := reasoningModel(t)
	m = reasoningUIStart(t, m, 0)
	m = reasoningUIDelta(t, m, 0, 0, "body")
	m.detailOpen = true
	m.mergeLiveReasoning()
	m.browser.selected = 0
	m.browser.detail = true
	m.prepareLiveReasoningPage()
	m, _ = browserKey(t, m, tea.KeyCtrlC)
	if !strings.Contains(m.View(), "Cancelling") || strings.Contains(m.View(), "Thinking") {
		t.Fatal("reader concealed cancellation", m.View())
	}
	m = reasoningUIComplete(t, m, 0, "body")
	if !strings.Contains(m.View(), "Cancelling") || strings.Contains(m.View(), "waiting to save") {
		t.Fatal("late boundary covered cancellation", m.View())
	}
}

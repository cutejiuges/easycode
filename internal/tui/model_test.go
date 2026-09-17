package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/protocol"
)

func TestModelProjectsWindowAndRuntimeEvent(t *testing.T) {
	model := NewModel("test")
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(Model)
	updated, _ = model.Update(RuntimeEventMsg{Event: protocol.NewEvent(protocol.EventTurnStarted)})
	model = updated.(Model)

	view := model.View()
	if !strings.Contains(view, "120x40") {
		t.Fatalf("view does not contain window size: %s", view)
	}
	if !strings.Contains(view, "事件：1") {
		t.Fatalf("view does not contain event count: %s", view)
	}
}

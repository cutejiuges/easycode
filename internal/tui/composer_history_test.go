package tui

import (
	"context"
	"testing"

	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

func TestIdenticalHistoryRecallPreservesRewindDraftIntent(t *testing.T) {
	model := NewModel(context.Background(), "test", &fakeChatSession{})
	model.handoffEnabled = true
	model.composer.history.add("other committed prompt")
	model.composer.history.add("/rewind")
	if err := model.installRewindDraft(RewindDraft{Text: "/rewind"}); err != nil {
		t.Fatal(err)
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	model, submit := updateModelWithCommand(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if submit != nil || model.pending != nil || model.handoffIntent != nil || model.recalledSend != rewindBack ||
		model.composer.rewind.intent != domain.InputSkillLiteral {
		t.Fatal("identical history text lost its send guard or original intent")
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	if model.composer.text != "other committed prompt" || model.composer.rewind != (recalledDraft{}) {
		t.Fatal("selecting different history kept the historical draft intent")
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if model.composer.text != "/rewind" || !model.composer.rewind.unedited || model.composer.rewind.intent != domain.InputSkillLiteral {
		t.Fatal("returning to identical text did not restore its original intent")
	}
}

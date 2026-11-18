package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

// cancellingSkillSession 模拟准备阶段收到取消，取消后不能进入 admission。
type cancellingSkillSession struct {
	fakeChatSession
	admitted bool
}

func (s *cancellingSkillSession) Append(ctx context.Context, draft domain.InputDraft) (protocol.CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.CommandResult{}, err
	}
	s.admitted = true
	return s.fakeChatSession.Append(ctx, draft)
}
func TestSkillPreparationCancellationAndStaleResponse(t *testing.T) {
	session := &cancellingSkillSession{}
	model := NewModel(context.Background(), "test", session)
	model.composer.replace(0, 0, "/demo args")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	message := cmd()
	model = updateModel(t, model, message)
	if session.admitted || model.composer.text != "/demo args" || model.pending != nil {
		t.Fatal("cancelled preparation admitted or lost draft")
	}
	model.submissionGeneration++
	model.composer.replace(0, len(model.composer.text), "new draft")
	model = updateModel(t, model, message)
	if model.composer.text != "new draft" {
		t.Fatal("stale failure overwrote draft")
	}
}

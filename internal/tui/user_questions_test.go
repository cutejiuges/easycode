package tui

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type questionChatFixture struct {
	fakeChatSession
	answers []protocol.QuestionAnswerSet
	err     error
}

func (s *questionChatFixture) ResolveQuestion(_ context.Context, _ domain.TurnID, _ protocol.UserQuestionRequestedPayload, a protocol.QuestionAnswerSet) error {
	s.answers = append(s.answers, a)
	return s.err
}
func questionUIEvent(t *testing.T, count int) protocol.Event {
	t.Helper()
	questions := make([]protocol.Question, count)
	for i := range questions {
		questions[i] = protocol.Question{ID: protocol.QuestionID(fmt.Sprintf("question_%d", i)), Header: "存储 📦", Question: fmt.Sprintf("选择存储方式 %d？\x1b[2J保持已有数据", i+1), MultiSelect: i%2 == 1, Options: []protocol.QuestionOption{{Label: "SQLite (Recommended)", Description: "本地文件，便于备份和恢复。"}, {Label: "PostgreSQL", Description: "独立服务，多实例共享。"}, {Label: "Memory", Description: "临时运行时状态。"}}}
	}
	in, err := protocol.NewQuestionInput(questions)
	if err != nil {
		t.Fatal(err)
	}
	e, err := protocol.NewUserQuestionRequested(protocol.UserQuestionRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000001", InvocationID: "01900000-0000-7000-8000-000000000002", Fingerprint: in.Fingerprint(), Input: in, ResultAllowance: 128 << 10})
	if err != nil {
		t.Fatal(err)
	}
	e.TurnID = "01900000-0000-7000-8000-000000000003"
	return e
}
func questionUIModel(t *testing.T, count int, s *questionChatFixture) Model {
	t.Helper()
	m := NewModel(context.Background(), "test", s)
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	return updateModel(t, m, RuntimeEventMsg{Event: questionUIEvent(t, count)})
}
func questionKey(t *testing.T, m Model, k tea.KeyType) Model {
	t.Helper()
	return updateModel(t, m, tea.KeyMsg{Type: k})
}
func TestQuestionChoosingNotesReviewAndSingleSubmission(t *testing.T) {
	s := &questionChatFixture{}
	m := questionUIModel(t, 4, s)
	m.composer = composerWithText(t, "chat draft")
	chat := m.composer
	m = questionKey(t, m, tea.KeyDown)
	m = questionKey(t, m, tea.KeyTab)
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" exact 说明\n"), Paste: true})
	m = questionKey(t, m, tea.KeyEsc)
	if m.question.editing || s.interrupts != 0 {
		t.Fatal("Esc did not return to choices")
	}
	m = questionKey(t, m, tea.KeyEnter)
	m = questionKey(t, m, tea.KeySpace)
	m = questionKey(t, m, tea.KeyDown)
	m = questionKey(t, m, tea.KeySpace)
	m = questionKey(t, m, tea.KeyEnter)
	for i := 0; i < 3; i++ {
		m = questionKey(t, m, tea.KeyDown)
	}
	m = questionKey(t, m, tea.KeyEnter)
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  自定义 🧑‍💻"), Paste: true})
	m = questionKey(t, m, tea.KeyEnter)
	for i := 0; i < 4; i++ {
		m = questionKey(t, m, tea.KeyDown)
	}
	m = questionKey(t, m, tea.KeyEnter)
	if !m.question.reviewing || len(s.answers) != 0 {
		t.Fatal("answers submitted without review")
	}
	a, err := m.question.answers()
	if err != nil {
		t.Fatal(err)
	}
	got := a.Answers()
	if !reflect.DeepEqual(got[0].SelectedOptions, []int{1}) || got[0].Text != " exact 说明\n" || !reflect.DeepEqual(got[1].SelectedOptions, []int{0, 1}) || len(got[2].SelectedOptions) != 0 || got[2].Text != "  自定义 🧑‍💻" || !got[3].Skipped {
		t.Fatalf("answers=%#v", got)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || !m.question.submitted {
		t.Fatal("review did not submit")
	}
	m = updateModel(t, m, cmd())
	if m.question == nil || len(s.answers) != 1 {
		t.Fatal("admission closed question before durable resolution")
	}
	_, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if duplicate != nil || !reflect.DeepEqual(chat, m.composer) {
		t.Fatal("duplicate submit or changed chat draft")
	}
	p := m.question.request
	resolved, _ := protocol.NewUserQuestionResolved(protocol.UserQuestionResolvedPayload{RequestID: p.RequestID, InvocationID: p.InvocationID, Fingerprint: p.Fingerprint, Resolution: protocol.QuestionAnswered})
	resolved.TurnID = m.question.turnID
	m = updateModel(t, m, RuntimeEventMsg{Event: resolved})
	if m.question != nil || !reflect.DeepEqual(chat, m.composer) {
		t.Fatal("resolution lost chat state")
	}
}
func TestQuestionUnansweredSkipRejectionAndStaleMessages(t *testing.T) {
	s := &questionChatFixture{err: fmt.Errorf("question_answer_invalid")}
	m := questionUIModel(t, 1, s)
	m = questionKey(t, m, tea.KeyRight)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd != nil || m.question.errorText == "" {
		t.Fatal("unanswered question invented a default")
	}
	m = questionKey(t, m, tea.KeyDown)
	m = questionKey(t, m, tea.KeyEnter)
	if !m.question.drafts[0].skipped || m.question.submitted || len(s.answers) != 0 {
		t.Fatal("skip all submitted without review")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	m = updateModel(t, m, cmd())
	if m.question.submitted || m.question.errorText == "" || !m.question.drafts[0].skipped {
		t.Fatal("rejection discarded draft")
	}
	p := m.question.request
	oldID := p.RequestID
	p.RequestID = "01900000-0000-7000-8000-000000000009"
	m.question.request = p
	m = updateModel(t, m, questionSubmitResult{requestID: oldID, err: fmt.Errorf("old rejection")})
	resolved, _ := protocol.NewUserQuestionResolved(protocol.UserQuestionResolvedPayload{RequestID: oldID, InvocationID: p.InvocationID, Fingerprint: p.Fingerprint, Resolution: protocol.QuestionSkipped})
	resolved.TurnID = m.question.turnID
	m = updateModel(t, m, RuntimeEventMsg{Event: resolved})
	if m.question == nil || m.question.request.RequestID != p.RequestID || m.question.errorText == "old rejection" {
		t.Fatal("stale message changed current request")
	}
}

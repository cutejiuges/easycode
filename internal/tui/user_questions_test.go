package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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
func TestQuestionPasteBoundariesResizeAndStop(t *testing.T) {
	s := &questionChatFixture{}
	m := questionUIModel(t, 1, s)
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlC, Paste: true})
	if m.question.stopping {
		t.Fatal("paste stopped question")
	}
	m = questionKey(t, m, tea.KeyTab)
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("x", protocol.MaxQuestionTextBytes)), Paste: true})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("overflow"), Paste: true})
	if len(m.question.drafts[0].notes.text) != protocol.MaxQuestionTextBytes || m.question.errorText == "" {
		t.Fatal("paste overflow changed accepted draft")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 12, Height: 6})
	if len(m.question.drafts[0].notes.text) != protocol.MaxQuestionTextBytes {
		t.Fatal("resize lost draft")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)
	if cmd == nil || !m.question.stopping {
		t.Fatal("stop did not retain disabled overlay")
	}
	cmd()
	_, again := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if again != nil || s.interrupts != 1 {
		t.Fatal("duplicate stop")
	}
}
func TestQuestionSnapshotsAndReachableActions(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {34, 12}, {12, 6}} {
		for _, review := range []bool{false, true} {
			name := fmt.Sprintf("question_%dx%d_%t.golden", size.w, size.h, review)
			t.Run(name, func(t *testing.T) {
				m := questionUIModel(t, 1, &questionChatFixture{})
				m = updateModel(t, m, tea.WindowSizeMsg{Width: size.w, Height: size.h})
				if review {
					m = questionKey(t, m, tea.KeyEnter)
				}
				got := m.View()
				want, err := os.ReadFile(filepath.Join("testdata", name))
				if err != nil {
					t.Fatal(err)
				}
				if got != string(want) {
					t.Fatalf("snapshot mismatch\n%s", got)
				}
				if strings.Contains(got, "\x1b") {
					t.Fatal("terminal control sequence leaked")
				}
				for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
					if ansi.StringWidth(line) > size.w {
						t.Fatal("line exceeds width", line)
					}
				}
				if strings.Count(got, "\n") > size.h {
					t.Fatal("view exceeds height")
				}
				if review {
					if !strings.Contains(got, "Submit") {
						t.Fatal("submit action unreachable")
					}
				} else {
					for i := 0; i < 4; i++ {
						m = questionKey(t, m, tea.KeyDown)
					}
					if !strings.Contains(m.View(), "Skip") {
						t.Fatal("skip action unreachable")
					}
				}
				before := m.question.drafts[0]
				for i := 0; i < 3; i++ {
					_ = m.View()
				}
				if !reflect.DeepEqual(before, m.question.drafts[0]) {
					t.Fatal("View mutated answer")
				}
			})
		}
	}
}

type questionBrowserFixture struct{ browserQueries }

func (*questionBrowserFixture) ListTranscript(context.Context, protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	return protocol.NewTranscriptListPage(protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{{Cursor: protocol.TranscriptCursor{Sequence: 8}, Kind: domain.TranscriptCall, TurnID: "01900000-0000-7000-8000-000000000003", Activity: protocol.ToolActivityPayload{Tool: protocol.ToolQuestion, InvocationID: "01900000-0000-7000-8000-000000000002", Summary: "Choose?", Status: "success", Code: "ok"}, Preview: "Answered 1 · Skipped 0"}}})
}
func (*questionBrowserFixture) ReadTranscriptBody(_ context.Context, q protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	text := "Choose?\nSQLite\n exact answer\n"
	if q.Source == protocol.TranscriptInput {
		text = "Choose?\nSQLite\nRemote\n"
	}
	return protocol.NewTranscriptBodyPage(protocol.TranscriptBodyPage{Text: text, NextOffset: uint64(len(text)), TotalBytes: uint64(len(text))})
}
func TestQuestionHistoryIsReadOnlyAndBreaksReadGroups(t *testing.T) {
	read := protocol.ToolActivityPayload{Tool: protocol.ToolRead, Status: "success", Target: "a"}
	question := protocol.ToolActivityPayload{Tool: protocol.ToolQuestion, Status: "success", Summary: "Choose?"}
	lines := renderTranscript([]transcriptMessage{{activity: &read}, {activity: &question, preview: "Answered 1 · Skipped 0"}, {activity: &read}}, 80)
	if strings.Count(strings.Join(lines, "\n"), "Read 1 files") != 2 {
		t.Fatal("question did not break read group")
	}
	s := &questionChatFixture{}
	m := NewModel(context.Background(), "test", s).WithTranscriptQueries("01900000-0000-7000-8000-000000000004", &questionBrowserFixture{})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	for _, key := range []tea.KeyType{tea.KeyCtrlO, tea.KeyEnter, tea.KeyLeft, tea.KeyRight} {
		updated, cmd := m.Update(tea.KeyMsg{Type: key})
		m = updated.(Model)
		if cmd != nil {
			m = updateModel(t, m, cmd())
		}
		if m.question != nil || len(s.answers) != 0 || len(m.browser.cache) > 4 {
			t.Fatal("history reopened question or escaped page budget")
		}
	}
	if !strings.Contains(m.View(), "exact answer") {
		t.Fatal("question result not visible in history")
	}
}

func TestQuestionLongContentRemainsReachableAfterResize(t *testing.T) {
	m := questionUIModel(t, 1, &questionChatFixture{})
	questions := m.question.request.Input.Questions()
	questions[0].Question = strings.Repeat("q", 2048)
	questions[0].Options[0].Label = strings.Repeat("L", 128)
	questions[0].Options[0].Description = strings.Repeat("界", 341)
	in, err := protocol.NewQuestionInput(questions)
	if err != nil {
		t.Fatal(err)
	}
	m.question.request.Input = in
	m.question.request.Fingerprint = in.Fingerprint()
	for _, size := range [][2]int{{80, 24}, {34, 12}, {12, 6}, {80, 24}} {
		m = updateModel(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.question.focus = 4
		m.question.reveal(size[0], size[1])
		if !strings.Contains(m.View(), "Skip") {
			t.Fatal("long question hid skip action")
		}
		m = questionKey(t, m, tea.KeyPgUp)
		m.question.drafts[0] = questionDraft{selected: []int{0}, answered: true, notes: composerWithText(t, strings.Repeat("x", protocol.MaxQuestionTextBytes))}
		m.question.reviewing = true
		m.question.focus = 1
		m.question.reveal(size[0], size[1])
		if !strings.Contains(m.View(), "Submit") {
			t.Fatal("long answer hid submit action")
		}
		if m.question.request.Input.Fingerprint() != in.Fingerprint() || len(m.question.drafts[0].notes.text) != protocol.MaxQuestionTextBytes {
			t.Fatal("resize changed question or answer")
		}
		if len(strings.Split(m.View(), "\n")) > size[1] {
			t.Fatal("long view exceeded terminal height")
		}
		m.question.reviewing = false
	}
}

func TestQuestionEditingKeepsCursorVisibleAndResetUsesCurrentSize(t *testing.T) {
	m := questionUIModel(t, 1, &questionChatFixture{})
	qs := m.question.request.Input.Questions()
	qs[0].Question = strings.Repeat("q", 2048)
	in, _ := protocol.NewQuestionInput(qs)
	m.question.request.Input = in
	m.question.request.Fingerprint = in.Fingerprint()
	m.question.drafts[0].notes = composerWithText(t, strings.Repeat("line\n", 20)+"CURSOR_END")
	m.question.editing = true
	m.question.resize(80, 24)
	if !strings.Contains(m.View(), "CURSOR_END|") {
		t.Fatal("long question hid current input cursor")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 12, Height: 6})
	m.question.editing = false
	m.question.reviewing = true
	m.question.focus = 2
	m = questionKey(t, m, tea.KeyEnter)
	if m.question.drafts[0].notes.width != 12 || m.question.drafts[0].notes.height != 6 {
		t.Fatal("skip all reset composer to stale terminal size")
	}
	m.question.reviewing = false
	m.question.focus = 4
	m = questionKey(t, m, tea.KeyEnter)
	if m.question.drafts[0].notes.width != 12 {
		t.Fatal("skip question reset composer to stale size")
	}
}

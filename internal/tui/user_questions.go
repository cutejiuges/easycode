package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type questionSession interface {
	ResolveQuestion(context.Context, domain.TurnID, protocol.UserQuestionRequestedPayload, protocol.QuestionAnswerSet) error
}
type questionDraft struct {
	selected []int
	notes    composer
	answered bool
	skipped  bool
}
type questionOverlay struct {
	request                                 protocol.UserQuestionRequestedPayload
	turnID                                  domain.TurnID
	drafts                                  []questionDraft
	current, focus, scroll                  int
	editing, reviewing, submitted, stopping bool
	errorText                               string
}
type questionSubmitResult struct {
	requestID domain.QuestionRequestID
	err       error
}

func newQuestionOverlay(p protocol.UserQuestionRequestedPayload, turnID domain.TurnID, width, height int) *questionOverlay {
	q := &questionOverlay{request: p, turnID: turnID, drafts: make([]questionDraft, len(p.Input.Questions()))}
	for i := range q.drafts {
		q.drafts[i].notes = newComposer()
	}
	q.resize(width, height)
	return q
}
func (q *questionOverlay) resize(width, height int) {
	for i := range q.drafts {
		q.drafts[i].notes.resize(max(1, width), max(5, height))
	}
	q.reveal(width, height)
}
func (q *questionOverlay) advance() {
	q.editing = false
	q.scroll = 0
	if q.current+1 < len(q.drafts) {
		q.current++
		q.focus = 0
	} else {
		q.reviewing = true
		q.focus = len(q.drafts)
	}
}
func (q *questionOverlay) answers() (protocol.QuestionAnswerSet, error) {
	as := make([]protocol.QuestionAnswer, len(q.drafts))
	for i, d := range q.drafts {
		if !d.answered {
			return protocol.QuestionAnswerSet{}, fmt.Errorf("answer each question or explicitly skip it")
		}
		as[i] = protocol.QuestionAnswer{QuestionID: q.request.Input.Questions()[i].ID, SelectedOptions: d.selected, Text: d.notes.text, Skipped: d.skipped}
	}
	a, err := protocol.NewQuestionAnswerSet(as)
	if err != nil {
		return protocol.QuestionAnswerSet{}, err
	}
	return a, a.ValidateAgainst(q.request.Input)
}

func (model Model) handleQuestionKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	q := model.question
	if q.stopping {
		return model, nil
	}
	if !key.Paste && (key.Type == tea.KeyCtrlC || (key.Type == tea.KeyEsc && (!q.editing || q.submitted))) {
		q.submitted = true
		q.stopping = true
		return model, interruptSession(model.session)
	}
	if q.submitted {
		return model, nil
	}
	if !key.Paste && (key.Type == tea.KeyPgUp || key.Type == tea.KeyPgDown) {
		delta := max(1, model.heightOrDefault()-3)
		if key.Type == tea.KeyPgUp {
			delta = -delta
		}
		q.scroll = max(0, q.scroll+delta)
		return model, nil
	}
	q.errorText = ""
	if q.editing {
		if !key.Paste && (key.Type == tea.KeyEsc || key.Type == tea.KeyTab) {
			q.editing = false
			q.reveal(model.widthOrDefault(), model.heightOrDefault())
			return model, nil
		}
		d := &q.drafts[q.current]
		if !key.Paste && key.Type == tea.KeyEnter && !key.Alt && strings.TrimSpace(d.notes.text) == "" && len(d.selected) > 0 {
			d.answered = true
			q.advance()
			q.reveal(model.widthOrDefault(), model.heightOrDefault())
			return model, nil
		}
		candidate := d.notes
		intent, err := candidate.input(key)
		total := len(candidate.text)
		for i, other := range q.drafts {
			if i != q.current {
				total += len(other.notes.text)
			}
		}
		if err != nil || total > protocol.MaxQuestionTextBytes {
			q.errorText = "answer exceeds text limit"
			return model, nil
		}
		d.notes = candidate
		d.skipped = false
		d.answered = len(d.selected) > 0 || strings.TrimSpace(d.notes.text) != ""
		if intent == composerSubmit {
			q.advance()
		}
		q.reveal(model.widthOrDefault(), model.heightOrDefault())
		return model, nil
	}
	if key.Paste {
		return model, nil
	}
	if key.Type == tea.KeyShiftTab {
		if q.reviewing {
			q.reviewing = false
			q.current = len(q.drafts) - 1
		} else if q.current > 0 {
			q.current--
		}
		q.focus = 0
		q.scroll = 0
		return model, nil
	}
	if q.reviewing {
		switch key.Type {
		case tea.KeyUp:
			q.focus = (q.focus + len(q.drafts) + 1) % (len(q.drafts) + 2)
		case tea.KeyDown:
			q.focus = (q.focus + 1) % (len(q.drafts) + 2)
		case tea.KeyEnter:
			if q.focus < len(q.drafts) {
				q.current = q.focus
				q.reviewing = false
				q.focus = 0
				q.scroll = 0
			} else if q.focus == len(q.drafts)+1 {
				for i := range q.drafts {
					q.drafts[i].selected = nil
					q.drafts[i].notes = newComposer()
					q.drafts[i].notes.resize(model.widthOrDefault(), max(5, model.heightOrDefault()))
					q.drafts[i].skipped = true
					q.drafts[i].answered = true
				}
				q.focus = len(q.drafts)
			} else {
				return model, model.submitQuestion()
			}
		}
		q.reveal(model.widthOrDefault(), model.heightOrDefault())
		return model, nil
	}
	question := q.request.Input.Questions()[q.current]
	d := &q.drafts[q.current]
	switch key.Type {
	case tea.KeyUp:
		q.focus = (q.focus + len(question.Options) + 1) % (len(question.Options) + 2)
	case tea.KeyDown:
		q.focus = (q.focus + 1) % (len(question.Options) + 2)
	case tea.KeyRight:
		q.advance()
	case tea.KeyTab:
		q.editing = true
	case tea.KeySpace:
		if question.MultiSelect && q.focus < len(question.Options) {
			d.selected = toggleQuestionOption(d.selected, q.focus)
			d.skipped = false
			d.answered = len(d.selected) > 0 || strings.TrimSpace(d.notes.text) != ""
		}
	case tea.KeyEnter:
		switch {
		case q.focus == len(question.Options):
			d.selected = nil
			d.skipped = false
			q.editing = true
		case q.focus == len(question.Options)+1:
			d.selected = nil
			d.notes = newComposer()
			d.notes.resize(model.widthOrDefault(), max(5, model.heightOrDefault()))
			d.skipped = true
			d.answered = true
			q.advance()
		case question.MultiSelect:
			if len(d.selected) == 0 && strings.TrimSpace(d.notes.text) == "" {
				q.errorText = "select an option or explicitly skip"
			} else {
				d.answered = true
				d.skipped = false
				q.advance()
			}
		default:
			d.selected = []int{q.focus}
			d.answered = true
			d.skipped = false
			q.advance()
		}
	}
	q.reveal(model.widthOrDefault(), model.heightOrDefault())
	return model, nil
}
func toggleQuestionOption(indices []int, index int) []int {
	next := make([]int, 0, len(indices)+1)
	found := false
	for _, v := range indices {
		if v == index {
			found = true
		} else {
			next = append(next, v)
		}
	}
	if !found {
		next = append(next, index)
	}
	sort.Ints(next)
	return next
}
func (model *Model) submitQuestion() tea.Cmd {
	q := model.question
	answers, err := q.answers()
	if err != nil {
		q.errorText = err.Error()
		return nil
	}
	target, ok := model.session.(questionSession)
	if !ok {
		q.errorText = "interactive user input is unavailable"
		return nil
	}
	q.submitted = true
	request, turnID, ctx := q.request, q.turnID, model.submitContext
	return func() tea.Msg {
		err := target.ResolveQuestion(ctx, turnID, request, answers)
		return questionSubmitResult{request.RequestID, err}
	}
}

func (q *questionOverlay) body(width int) ([]string, int) {
	var lines []string
	focusLine := 0
	add := func(s string) { lines = append(lines, wrapApprovalDiff(commandDisplayText(s), max(1, width))...) }
	if q.reviewing {
		questions := q.request.Input.Questions()
		for i, d := range q.drafts {
			prefix := "  "
			if q.focus == i {
				prefix = "> "
				focusLine = len(lines)
			}
			add(fmt.Sprintf("%s%d. %s", prefix, i+1, questions[i].Question))
			if !d.answered {
				add("  Unanswered")
			} else if d.skipped {
				add("  Skipped")
			} else {
				for _, index := range d.selected {
					add("  ✓ " + questions[i].Options[index].Label)
				}
				if d.notes.text != "" {
					add("  " + d.notes.text)
				}
			}
		}
		for i, label := range []string{"Submit answers", "Skip all questions"} {
			prefix := "  "
			if q.focus == len(q.drafts)+i {
				prefix = "> "
				focusLine = len(lines)
			}
			add(prefix + label)
		}
		return lines, focusLine
	}
	question := q.request.Input.Questions()[q.current]
	d := q.drafts[q.current]
	add(question.Header)
	add(question.Question)
	if q.editing {
		add("Notes / other answer:")
		focusLine = len(lines) + d.notes.rowIndex() - d.notes.top
		body := d.notes.inputView(true)
		lines = append(lines, strings.Split(strings.TrimSuffix(body, "\n"), "\n")...)
		return lines, focusLine
	}
	for i, option := range question.Options {
		prefix := "  "
		if q.focus == i {
			prefix = "> "
			focusLine = len(lines)
		}
		mark := "( )"
		for _, selected := range d.selected {
			if selected == i {
				mark = "(x)"
			}
		}
		add(prefix + mark + " " + option.Label)
		add("    " + option.Description)
	}
	for i, label := range []string{"Other: type an answer", "Skip this question"} {
		prefix := "  "
		if q.focus == len(question.Options)+i {
			prefix = "> "
			focusLine = len(lines)
		}
		add(prefix + label)
	}
	if d.notes.text != "" {
		add("Notes: " + d.notes.text)
	}
	return lines, focusLine
}
func (q *questionOverlay) reveal(width, height int) {
	lines, focus := q.body(max(1, width))
	visible := max(1, height-3)
	q.scroll = min(q.scroll, focus)
	q.scroll = max(q.scroll, focus-visible+1)
	q.scroll = max(0, min(q.scroll, len(lines)-visible))
}
func (model Model) renderQuestion() string {
	q := model.question
	width, height := model.widthOrDefault(), model.heightOrDefault()
	if height < 3 {
		return ansi.Truncate("Question", width, "") + "\n"
	}
	title := fmt.Sprintf("Question %d/%d", q.current+1, len(q.drafts))
	if q.reviewing {
		title = "Review your answers"
	}
	if q.submitted {
		title = "Submitting answers…"
	}
	if q.stopping {
		title = "Stopping…"
	}
	footer := "↑↓ choose · Enter next · Tab notes · Shift+Tab back · PgUp/PgDn · Esc stop"
	if q.request.Input.Questions()[q.current].MultiSelect {
		footer = "↑↓ · Space toggle · Enter next · Tab notes · Shift+Tab back · PgUp/PgDn · Esc stop"
	}
	if q.reviewing {
		footer = "↑/↓ select · Enter confirm · Shift+Tab back · PgUp/PgDn scroll · Esc stop"
	}
	if q.editing {
		footer = "Enter next · Alt+Enter newline · Tab/Esc back · Ctrl+C stop"
	}
	if width < 80 {
		footer = "↑↓ Enter · Tab notes · Esc stop"
		if q.reviewing {
			footer = "↑↓ Enter · Shift+Tab · Esc stop"
		}
		if q.editing {
			footer = "Enter next · Tab back · ^C stop"
		}
	}
	if width < 34 {
		footer = "↑↓ Enter Esc"
		if q.editing {
			footer = "Enter · Esc"
		}
	}
	var result []string
	result = append(result, strings.TrimRight(ansi.Truncate(title, width, ""), " "))
	lines, _ := q.body(width)
	visible := height - 2
	if q.errorText != "" {
		visible--
	}
	start := max(0, min(q.scroll, len(lines)-max(1, visible)))
	end := min(len(lines), start+max(1, visible))
	result = append(result, lines[start:end]...)
	for len(result) < height-1 && q.errorText == "" {
		result = append(result, "")
	}
	if q.errorText != "" {
		result = append(result, ansi.Truncate(commandDisplayText(q.errorText), width, ""))
	}
	result = append(result, ansi.Truncate(footer, width, ""))
	return strings.Join(result, "\n")
}

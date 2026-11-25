package tui

import (
	"strings"

	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// recalledDraft 将重填的首词意图随编辑快照保存，普通草稿使用零值。
type recalledDraft struct {
	head     string
	intent   domain.InputSkillIntent
	unedited bool
}

func (draft *recalledDraft) edited(text string) {
	draft.unedited = false
	if draft.head != "" {
		head, _ := domain.SplitInputHead(text)
		if draft.head != head {
			*draft = recalledDraft{}
		}
	}
}

func (m *Model) installRewindDraft(draft RewindDraft) error {
	if err := domain.ValidateUserText(draft.Text); err != nil {
		return err
	}
	if err := m.composer.replace(0, len(m.composer.text), draft.Text); err != nil {
		return err
	}
	m.composer.rewind = recalledDraft{unedited: true}
	head, _ := domain.SplitInputHead(m.composer.text)
	if strings.HasPrefix(head, "/") || strings.HasPrefix(head, "$") {
		m.composer.rewind.head = strings.Clone(head)
		m.composer.rewind.intent = domain.InputSkillLiteral
		if draft.HasSkill {
			m.composer.rewind.intent = domain.InputSkillRequired
		}
	}
	m.completion.suppressPastedInput(m.composer)
	m.suppressPastedFiles()
	return nil
}

const rewindSendLabel = "Send unchanged"

func (m Model) handleRecalledSendKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste || key.Alt || key.Type == tea.KeyCtrlJ {
		return m, nil
	}
	switch key.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.recalledSend = rewindBrowsing
	case tea.KeyEnter:
		send := m.recalledSend == rewindProceed && m.recalledSendFits()
		m.recalledSend = rewindBrowsing
		if send {
			return m.submitComposer("", "")
		}
	default:
		if m.recalledSendFits() {
			m.recalledSend.key(key.Type)
		}
	}
	return m, nil
}

func (m Model) recalledSendFits() bool {
	return m.widthOrDefault() >= len("> "+rewindSendLabel) && m.heightOrDefault() >= 4
}

func (m Model) renderRecalledSend() string {
	width, height := m.widthOrDefault(), m.heightOrDefault()
	if !m.recalledSendFits() {
		return pickerWrapped("> Back\nResize to send unchanged", width, height, 0)
	}
	lines := []string{"Send this prompt without editing?", ansi.Truncate(pickerSafe(m.composer.text, 160), width, "…")}
	lines = append(lines, m.recalledSend.actions(rewindSendLabel)...)
	if len(lines) < height {
		lines = append(lines, "↑↓ choose · Enter confirm · Esc back")
	}
	return fitPickerLines(lines, width)
}

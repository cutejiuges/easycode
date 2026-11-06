package tui

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

type completionSuppression uint8

const (
	completionNotSuppressed completionSuppression = iota
	completionDismissed
	completionPasted
	completionModal
	completionAccepted
	completionCancelled
)

type completionState struct {
	catalog         *completionCatalog
	target          completionTarget
	valid, parsed   bool
	revision        uint64
	cursor          int
	suppression     completionSuppression
	suppressedToken string
	suppressedStart int
	matches         []completionMatch
	selected, top   int
	view            string
	rows            int
	layoutKey       completionLayoutKey
	layoutReady     bool
	generation      uint64
}

func (m Model) completionBlocked() bool {
	return m.question != nil || m.hookTrust != nil || m.hooks.open || m.skillsOpen ||
		m.approval != nil || m.cachePanel != nil || m.contextPanel != nil || m.detailOpen ||
		m.pending != nil || m.retracting != nil || m.cancelling || m.pendingContext != ""
}

func (s *completionState) refreshTarget(c composer) {
	if s.parsed && s.revision == c.revision && s.cursor == c.cursor {
		return
	}
	previous, previousValid := s.target, s.valid
	var selected completionIdentity
	if s.catalog != nil && len(s.matches) > 0 {
		selected = s.catalog.entries[s.matches[s.selected].index].id
	}
	s.target, s.valid = findCompletionTarget(c.text, c.cursor)
	s.parsed, s.revision, s.cursor = true, c.revision, c.cursor
	if !s.valid {
		s.matches = nil
		s.selected, s.top = 0, 0
		s.layoutReady = false
		return
	}
	if s.target.token != s.suppressedToken || s.target.start != s.suppressedStart {
		s.suppression = completionNotSuppressed
	}
	if previousValid && previous.start == s.target.start && previous.sigil == s.target.sigil && previous.query == s.target.query {
		return
	}
	s.matches = s.catalog.match(s.target)
	s.selected, s.top = 0, 0
	if previousValid && previous.start == s.target.start && previous.sigil == s.target.sigil {
		for i, match := range s.matches {
			if s.catalog.entries[match.index].id == selected {
				s.selected = i
				break
			}
		}
	}
	s.generation++
	s.layoutReady = false
}

func (s *completionState) suppress(reason completionSuppression) {
	if s.valid {
		s.suppressedToken = strings.Clone(s.target.token)
		s.suppressedStart = s.target.start
		s.suppression = reason
	}
	s.view, s.rows, s.layoutReady = "", 0, false
}

func (s *completionState) suppressPastedInput(c composer) {
	s.refreshTarget(c)
	// 粘贴把光标移入参数时，仍绑定首词，返回名称区不能接受粘贴前的候选。
	start := len(c.text) - len(strings.TrimLeftFunc(c.text, unicode.IsSpace))
	if target, ok := findCompletionTarget(c.text, start+1); ok {
		s.suppressedToken = target.token
		s.suppressedStart = target.start
		s.suppression = completionPasted
	}
	s.view, s.rows, s.layoutReady = "", 0, false
}

func (m *Model) prepareCompletion() {
	s := &m.completion
	s.refreshTarget(m.composer)
	if m.completionBlocked() {
		s.suppress(completionModal)
		return
	}
	if !s.valid || s.suppression != completionNotSuppressed || len(s.matches) == 0 {
		s.view, s.rows = "", 0
		return
	}
	m.layoutCompletion()
}

// handleCompletionKey 只返回编辑结果或共享提交意图，不访问运行时。
func (m *Model) handleCompletionKey(key tea.KeyMsg) (handled, submit bool) {
	s := &m.completion
	if key.Paste || key.Alt || key.Type == tea.KeyCtrlJ {
		return false, false
	}
	if key.Type == tea.KeyCtrlC {
		s.suppress(completionCancelled)
		return false, false
	}
	if key.Type == tea.KeyTab && s.valid && s.suppression != completionNotSuppressed &&
		s.suppression != completionAccepted && s.suppression != completionCancelled {
		s.suppression = completionNotSuppressed
		m.prepareCompletion()
		return s.rows > 0, false
	}
	if s.rows == 0 {
		return false, false
	}
	switch key.Type {
	case tea.KeyEsc:
		s.suppress(completionDismissed)
		return true, false
	case tea.KeyUp:
		s.selected = (s.selected + len(s.matches) - 1) % len(s.matches)
		return true, false
	case tea.KeyDown:
		s.selected = (s.selected + 1) % len(s.matches)
		return true, false
	case tea.KeyTab, tea.KeyEnter:
		candidate := s.catalog.entries[s.matches[s.selected].index]
		if err := m.composer.complete(s.target, candidate); err != nil {
			m.errorSummary = "invalid_input: " + err.Error()
			return true, false
		}
		// 接受后的光标已位于分隔符之后；保留短名称身份防止移动时误重开。
		s.suppressedToken = string(s.target.sigil) + candidate.id.name
		s.suppressedStart = s.target.start
		s.suppression = completionAccepted
		s.view, s.rows, s.layoutReady = "", 0, false
		return true, key.Type == tea.KeyEnter && candidate.id.kind == completionBuiltin
	}
	return false, false
}

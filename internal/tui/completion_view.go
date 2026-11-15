package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// minCompletionColumns 保证选择标记、可辨认的名称前缀及类型能同时出现。
const minCompletionColumns = len("> ") + 3 + 1 + len("[skill]")

type completionLayoutKey struct {
	generation             uint64
	width, space, selected int
	sigil                  byte
}

func completionBoundedText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := max(0, limit-len("…"))
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + "…"
}

func safeCompletionText(value string) string {
	return strings.ReplaceAll(safeComposerText(completionBoundedText(value, 1024)), "\n", " ↵ ")
}

func (m Model) completionSpace() int {
	return max(0, m.heightOrDefault()-m.composerChatRows()-renderedChatRows(m.renderInputs())-6-m.nonEffortStatusRows()-len(m.effortNoticeLines()))
}

func (m *Model) layoutCompletion() {
	s := &m.completion
	key := completionLayoutKey{s.generation, min(m.widthOrDefault(), maxCompletionLineBytes), m.completionSpace(), s.selected, s.target.sigil}
	if s.layoutReady && s.layoutKey == key {
		return
	}
	s.layoutReady, s.layoutKey = true, key
	if key.space == 0 || key.width < minCompletionColumns {
		s.view, s.rows = "", 0
		return
	}
	hint := key.space > 1
	count := min(5, len(s.matches), key.space)
	if hint {
		count = min(count, key.space-1)
	}
	s.top = min(s.top, s.selected)
	s.top = max(s.top, s.selected-count+1)
	s.top = max(0, min(s.top, len(s.matches)-count))
	lines := make([]string, 0, count+1)
	for i := s.top; i < s.top+count; i++ {
		candidate := s.catalog.entries[s.matches[i].index]
		marker := "  "
		if i == s.selected {
			marker = "> "
		}
		label := "command"
		if candidate.id.kind == completionSkill {
			label = "skill " + safeCompletionText(candidate.origin)
		}
		// 名称和类别/来源先分配列宽，长参数提示不能把来源挤出窄屏。
		labelBudget := max(7, key.width/2)
		if key.width >= 24 {
			labelBudget = max(16, labelBudget)
		}
		label = "[" + ansi.Truncate(label, max(1, labelBudget-2), "…") + "]"
		nameBudget := max(1, key.width-ansi.StringWidth(marker)-1-ansi.StringWidth(label))
		line := marker + ansi.Truncate(string(s.target.sigil)+candidate.id.name, nameBudget, "…") + " " + label
		for _, field := range []string{candidate.hint, candidate.description} {
			available := key.width - ansi.StringWidth(line) - 1
			if field != "" && available >= 4 {
				line += " " + ansi.Truncate(safeCompletionText(field), available, "…")
			}
		}
		line = completionBoundedText(line, maxCompletionLineBytes)
		lines = append(lines, ansi.Truncate(line, key.width, "…"))
	}
	if hint {
		line := fmt.Sprintf("%d/%d · Up/Down: select · Tab: fill · Enter: choose · Esc: close", s.selected+1, len(s.matches))
		lines = append(lines, ansi.Truncate(line, key.width, "…"))
	}
	s.rows = len(lines)
	s.view = strings.Join(lines, "\n") + "\n"
}

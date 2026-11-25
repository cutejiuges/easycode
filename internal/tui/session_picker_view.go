package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// pickerSafe 将不可信控制符转义为可见文本，先限制原文码点数。
func pickerSafe(text string, limit int) string {
	return pickerEscaped(text, limit, false)
}

func pickerEscaped(text string, limit int, layout bool) string {
	var b strings.Builder
	count := 0
	for _, r := range text {
		if count == limit {
			b.WriteString("…")
			break
		}
		count++
		if layout && (r == '\n' || r == '\t') {
			b.WriteRune(r)
		} else if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func previewBody(preview SessionPreview) string {
	const budget = 196608
	const marker = "\n[Preview truncated]"
	var b strings.Builder
	for _, entry := range preview.Messages {
		header := fmt.Sprintf("%s · %s · %s\n", entry.Message.Role, entry.TurnID, entry.Message.InputID)
		part := header + pickerEscaped(entry.Message.Text+fileSummaryText(entry.Files), 32768+8192, true)
		if entry.Truncated {
			part += "\n[Message truncated]"
		}
		part += "\n\n"
		if b.Len()+len(part) > budget {
			// 即使前一条恰好填满预算，也回收尾部空间以保留省略标记。
			prefix := b.String() + part
			end := budget - len(marker)
			for end > 0 && !utf8.RuneStart(prefix[end]) {
				end--
			}
			return prefix[:end] + marker
		}
		b.WriteString(part)
	}
	return b.String()
}

// View 只渲染当前拥有终端焦点的投影。
func (m StartupModel) View() string {
	if m.closing && m.finalNotice != "" {
		if m.request.Operation == PickerBranch {
			return pickerWrapped(pickerEscaped(m.finalNotice, 2048, true), m.width, m.height, 0)
		}
		return pickerWrapped(pickerSafe(m.finalNotice, 1024), m.width, m.height, 0)
	}
	switch m.phase {
	case shellChat:
		return m.chat.View()
	case shellOpening:
		if m.request.Operation == PickerRewind {
			return pickerWrapped("Loading rewind points…\nEsc: cancel", m.width, m.height, 0)
		}
		return pickerWrapped("Opening session chooser…\nEsc: cancel", m.width, m.height, 0)
	case shellRecovering:
		if m.request.Operation == PickerBranch {
			return pickerWrapped("Creating branch…\nEsc: cancel", m.width, m.height, 0)
		}
		return pickerWrapped("Resuming session…\nEsc: cancel", m.width, m.height, 0)
	case shellCancelling:
		if m.request.Operation == PickerBranch {
			return pickerWrapped("Cancelling branch — waiting for cleanup", m.width, m.height, 0)
		}
		if m.request.Operation == PickerRewind {
			return pickerWrapped("Cancelling rewind — waiting for cleanup", m.width, m.height, 0)
		}
		return pickerWrapped("Cancelling resume — waiting for cleanup", m.width, m.height, 0)
	case shellRetiring:
		return pickerWrapped("Switching sessions — closing previous session", m.width, m.height, 0)
	default:
		if m.rewind.browseID != 0 {
			return m.rewind.view(m.width, m.height)
		}
		return m.picker.view()
	}
}

func pickerWrapped(text string, width, height, scroll int) string {
	lines := strings.Split(ansi.Hardwrap(text, max(1, width), true), "\n")
	offset := min(scroll, max(0, len(lines)-height))
	return fitPickerLines(lines[offset:min(len(lines), offset+height)], width)
}

func (m pickerModel) view() string {
	if m.details {
		entry, _ := m.selection()
		return pickerWrapped(m.detailsText(entry), m.width, m.height, m.scroll)
	}
	lines := []string{"Resume a session"}
	if m.previewOpen {
		lines = append(lines, "Read-only preview · Esc back")
		if m.busy {
			lines = append(lines, "Loading preview…")
		} else if m.preview != nil {
			if title := m.preview.Title; title != "" {
				lines = append(lines, strings.Split(ansi.Hardwrap("Title: "+string(title), max(1, m.width), true), "\n")...)
			}
			if parent := m.preview.ParentThreadID; parent.Valid() {
				lines = append(lines, strings.Split(ansi.Hardwrap("Branch of: "+string(parent), max(1, m.width), true), "\n")...)
			}
			if len(m.preview.Messages) == 0 {
				lines = append(lines, "No visible text in this session")
			}
			if m.preview.Incomplete {
				lines = append(lines, "Incomplete journal · committed prefix only")
			}
			if m.preview.Truncated {
				lines = append(lines, "Preview truncated")
			}
			body := strings.Split(ansi.Hardwrap(previewBody(*m.preview), max(1, m.width), true), "\n")
			lines = append(lines, body...)
			offset := min(m.scroll, max(0, len(lines)-m.height))
			lines = lines[offset:]
		}
	} else if m.confirm {
		entry, _ := m.selection()
		text := "Session was created in another directory.\n" + pickerSafe(entry.CWD, 512) + "\nContinue using the current directory and its instructions?\n" + pickerSafe(m.config.CWD, 512) + "\n↑↓ scroll · Tab switch · Enter confirm · Esc back"
		body := strings.Split(ansi.Hardwrap(text, max(1, m.width), true), "\n")
		count := max(0, m.height-1)
		offset := min(m.scroll, max(0, len(body)-count))
		lines = append([]string{}, body[offset:min(len(body), offset+count)]...)
		if m.confirmProceed {
			lines = append(lines, "> Continue   Return")
		} else {
			lines = append(lines, "> Return     Continue")
		}
	} else {
		scope := pickerScope(m.all)
		lines = append(lines, scope+" · metadata search", "> "+pickerSafe(m.query, 256))
		if m.busy {
			lines = append(lines, "Loading…")
		}
		if !m.hasCurrentResults() && len(m.entries) > 0 {
			lines = append(lines, "Previous results: "+pickerScope(m.entriesAll)+" · resume disabled until refreshed")
		}
		if m.hasMore {
			lines = append(lines, "Latest 200 sessions only; search covers these results")
		}
		if m.warning != "" {
			lines = append(lines, strings.TrimSpace(m.warning))
		}
		if entry, ok := m.selection(); ok {
			identification := strings.Split(ansi.Hardwrap("ID: "+string(entry.ThreadID), max(1, m.width), true), "\n")
			status := "compatible"
			if !m.compatible(entry) {
				status = "incompatible"
			}
			identification = append(identification, pickerSafe(string(entry.Family)+" / "+entry.Wire, 256)+" · "+pickerSafe(entry.Model, 256)+" · "+status, pickerSafe(entry.CWD, 512))
			// 窄屏优先留出候选行，完整身份与元数据仍可在详情中查看。
			budget := max(0, m.height-len(lines)-3)
			lines = append(lines, identification[:min(len(identification), budget)]...)
		}
		rows := m.filtered()
		count := max(1, m.height-len(lines)-3)
		selected := 0
		for i, e := range rows {
			if e.ThreadID == m.selected {
				selected = i
			}
		}
		start := max(0, selected-count+1)
		for _, entry := range rows[start:min(len(rows), start+count)] {
			prefix := "  "
			if entry.ThreadID == m.selected {
				prefix = "> "
			}
			if entry.ParentThreadID.Valid() {
				prefix += "Branch · "
			}
			updated := m.pickerTime(entry.UpdatedAt)
			var line string
			if entry.Title != "" {
				metadata := updated + " " + pickerSafe(string(entry.Family)+" / "+entry.Model, 256)
				line = prefix + ansi.Truncate(string(entry.Title)+" · "+metadata, max(0, m.width-ansi.StringWidth(prefix)), "…")
			} else {
				line = prefix + updated + " " + pickerSafe(entry.Model, 256) + " " + pickerSafe(entry.CWD, 512)
			}
			lines = append(lines, line)
		}
		if len(rows) == 0 && m.hasCurrentResults() {
			if m.query == "" {
				if m.source.Valid() {
					lines = append(lines, "No other sessions in this scope")
				} else {
					lines = append(lines, "No sessions in this scope")
				}
			} else {
				lines = append(lines, "No matching sessions")
			}
		}
		exit := "Esc exit"
		if m.source.Valid() {
			exit = "Esc back"
		}
		lines = append(lines, "↑↓ select · Enter resume · Ctrl+V preview", "Ctrl+A scope · Ctrl+R refresh · Tab details · "+exit)
	}
	failure := m.failure
	if failure == "" {
		failure = m.inputFailure
	}
	if failure != "" {
		lines = append(lines[:min(len(lines), max(0, m.height-1))], failure)
	}
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	return fitPickerLines(lines, m.width)
}

func fitPickerLines(lines []string, width int) string {
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(1, width), "")
	}
	return strings.Join(lines, "\n")
}

func (m pickerModel) pickerTime(timestamp time.Time) string {
	location := m.config.Location
	if location == nil {
		location = time.UTC
	}
	return timestamp.In(location).Format("2006-01-02 15:04:05 -0700")
}

func (m pickerModel) detailsText(entry SessionEntry) string {
	text := string(entry.ThreadID) + "\n" + pickerSafe(string(entry.Family), 256) + " / " + pickerSafe(entry.Wire, 256) + "\n" + pickerSafe(entry.Model, 256) + "\nCreated in: " + pickerSafe(entry.CWD, 512)
	if entry.Title != "" {
		text = "Title: " + string(entry.Title) + "\nUpdated: " + m.pickerTime(entry.UpdatedAt) + "\n" + text
	}
	if parent := entry.ParentThreadID; parent.Valid() {
		text += "\nBranch of: " + string(parent)
	}
	if m.source.Valid() && entry.CWD != m.config.CWD {
		text += "\nOpen the creation directory with a compatible configuration and run:\neasycode --resume " + string(entry.ThreadID)
	} else {
		text += "\nResume in: " + pickerSafe(m.config.CWD, 512)
	}
	if m.source.Valid() && !m.compatible(entry) {
		text += "\n" + m.compatibilityDifference(entry)
	}
	return text + "\nEsc back · ↑↓ scroll"
}

func (m pickerModel) compatibilityDifference(e SessionEntry) string {
	var fields []string
	if e.Family != m.config.Family {
		fields = append(fields, "provider "+pickerSafe(string(e.Family), 256)+" != "+pickerSafe(string(m.config.Family), 256))
	}
	if e.Wire != m.config.Wire {
		fields = append(fields, "wire "+pickerSafe(e.Wire, 256)+" != "+pickerSafe(m.config.Wire, 256))
	}
	if e.Model != m.config.Model {
		fields = append(fields, "model "+pickerSafe(e.Model, 256)+" != "+pickerSafe(m.config.Model, 256))
	}
	return "Incompatible: " + strings.Join(fields, "; ")
}

func pickerScope(all bool) string {
	if all {
		return "All directories"
	}
	return "Current directory"
}

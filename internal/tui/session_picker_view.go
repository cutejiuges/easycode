package tui

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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
		part := header + pickerEscaped(entry.Message.Text, 32768, true)
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

// View 只渲染内存投影，任何尺寸都不访问存储。
func (m StartupModel) View() string {
	if m.chat != nil {
		return m.chat.View()
	}
	if m.details {
		entry, _ := m.selection()
		text := string(entry.ThreadID) + "\n" + pickerSafe(string(entry.Family), 256) + " / " + pickerSafe(entry.Wire, 256) + "\n" + pickerSafe(entry.Model, 256) + "\nCreated in: " + pickerSafe(entry.CWD, 512) + "\nResume in: " + pickerSafe(m.config.CWD, 512) + "\nEsc back · ↑↓ scroll"
		lines := strings.Split(ansi.Hardwrap(text, max(1, m.width), true), "\n")
		offset := min(m.scroll, max(0, len(lines)-m.height))
		return strings.Join(lines[offset:min(len(lines), offset+m.height)], "\n")
	}
	lines := []string{"Resume a session"}
	if m.previewOpen {
		lines = append(lines, "Read-only preview · Esc back")
		if m.busy {
			lines = append(lines, "Loading preview…")
		} else if m.preview != nil {
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
		if !m.loaded && len(m.entries) > 0 {
			lines = append(lines, "Previous results: "+pickerScope(m.entriesAll)+" · resume disabled until refreshed")
		}
		if m.hasMore {
			lines = append(lines, "Latest 200 sessions only; search covers these results")
		}
		if m.warning != "" {
			lines = append(lines, strings.TrimSpace(m.warning))
		}
		if entry, ok := m.selection(); ok {
			lines = append(lines, strings.Split(ansi.Hardwrap("ID: "+string(entry.ThreadID), max(1, m.width), true), "\n")...)
			status := "compatible"
			if !m.compatible(entry) {
				status = "incompatible"
			}
			lines = append(lines, pickerSafe(string(entry.Family)+" / "+entry.Wire, 256)+" · "+pickerSafe(entry.Model, 256)+" · "+status, pickerSafe(entry.CWD, 512))
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
			lines = append(lines, prefix+entry.UpdatedAt.In(m.location()).Format("2006-01-02 15:04:05 -0700")+" "+pickerSafe(entry.Model, 256)+" "+pickerSafe(entry.CWD, 512))
		}
		if len(rows) == 0 && m.loaded {
			if m.query == "" {
				lines = append(lines, "No sessions in this scope")
			} else {
				lines = append(lines, "No matching sessions")
			}
		}
		lines = append(lines, "↑↓ select · Enter resume · Ctrl+V preview", "Ctrl+A scope · Ctrl+R refresh · Tab details · Esc exit")
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
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(1, m.width), "")
	}
	return strings.Join(lines, "\n")
}

func (m StartupModel) location() *time.Location {
	if m.config.Location != nil {
		return m.config.Location
	}
	return time.UTC
}

func (m StartupModel) compatibilityDifference(e SessionEntry) string {
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

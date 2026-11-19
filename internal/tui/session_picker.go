package tui

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"
)

// PickerConfig 是启动时冻结的兼容性和目录信息。
type PickerConfig struct {
	Family           domain.ProviderFamily
	Wire, Model, CWD string
	All              bool
	Location         *time.Location
}

// pickerModel 只持有选择器投影和键盘意图，终端外层发起宿主操作。
type pickerModel struct {
	config                     PickerConfig
	source                     domain.ThreadID
	entries                    []SessionEntry
	entriesAll                 bool
	selected                   domain.ThreadID
	query                      string
	inputFailure               string
	all, loaded, busy, hasMore bool
	warning, failure           string
	preview                    *SessionPreview
	previewOpen                bool
	details                    bool
	confirm, confirmProceed    bool
	scroll, width, height      int
}

type pickerAction struct {
	operation     PickerOperation
	close         bool
	cancelPreview bool
}

func (m *pickerModel) key(v tea.KeyMsg) pickerAction {
	if v.Paste {
		v.Type, v.Alt = tea.KeyRunes, false
	}
	key := v.String()
	if key == "esc" {
		switch {
		case m.details:
			m.details = false
			m.scroll = 0
		case m.previewOpen:
			m.preview = nil
			m.previewOpen = false
			m.scroll = 0
			m.busy = false
			m.failure = ""
			return pickerAction{cancelPreview: true}
		case m.confirm:
			m.confirm = false
		case m.query != "":
			m.query = ""
			m.inputFailure = ""
			m.ensureSelection()
		default:
			return pickerAction{close: true}
		}
		return pickerAction{}
	}
	if m.previewOpen || m.details {
		m.scrollKey(key, max(1, m.height-4))
		return pickerAction{}
	}
	if m.confirm {
		if m.scrollKey(key, max(1, m.height-2)) {
			return pickerAction{}
		}
		switch key {
		case "left", "right", "tab":
			m.confirmProceed = !m.confirmProceed
		case "enter":
			m.confirm = false
			if m.confirmProceed {
				return pickerAction{operation: PickerResume}
			}
		}
		return pickerAction{}
	}
	switch key {
	case "tab":
		if m.selected != "" {
			m.details = true
			m.scroll = 0
		}
	case "ctrl+a":
		m.all = !m.all
		m.loaded = false
		return pickerAction{operation: PickerList}
	case "ctrl+r":
		m.loaded = false
		return pickerAction{operation: PickerRefresh}
	case "up":
		m.move(-1)
	case "down":
		m.move(1)
	case "pgup":
		m.move(-max(1, m.height-10))
	case "pgdown":
		m.move(max(1, m.height-10))
	case "ctrl+v":
		if m.loaded && m.selected != "" {
			m.previewOpen = true
			m.preview = nil
			m.scroll = 0
			return pickerAction{operation: PickerPreview}
		}
	case "enter":
		if m.loaded && !m.busy {
			return m.choose()
		}
	case "backspace":
		g := uniseg.NewGraphemes(m.query)
		end := 0
		for g.Next() {
			start, _ := g.Positions()
			end = start
		}
		m.query = m.query[:end]
		m.inputFailure = ""
		m.ensureSelection()
	default:
		if !v.Alt && (v.Type == tea.KeyRunes || v.Type == tea.KeySpace) {
			m.addQuery(v)
		}
	}
	return pickerAction{}
}

func (m *pickerModel) choose() pickerAction {
	entry, ok := m.selection()
	if !ok {
		return pickerAction{}
	}
	if m.source.Valid() && (entry.CWD != m.config.CWD || !m.compatible(entry)) {
		m.details = true
		m.scroll = 0
		return pickerAction{}
	}
	if !m.compatible(entry) {
		m.failure = m.compatibilityDifference(entry)
		return pickerAction{}
	}
	if entry.CWD != m.config.CWD {
		m.confirm = true
		m.scroll = 0
		m.confirmProceed = false
		return pickerAction{}
	}
	return pickerAction{operation: PickerResume}
}

func (m *pickerModel) scrollKey(key string, step int) bool {
	switch key {
	case "up":
		m.scroll = max(0, m.scroll-1)
	case "down":
		m.scroll++
	case "pgup":
		m.scroll = max(0, m.scroll-step)
	case "pgdown":
		m.scroll += step
	default:
		return false
	}
	return true
}

func (m *pickerModel) addQuery(key tea.KeyMsg) {
	addition := string(key.Runes)
	if key.Type == tea.KeySpace {
		addition = " "
	} else if key.Paste {
		addition = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(addition)
	}
	if !utf8.ValidString(addition) || strings.ContainsFunc(addition, unicode.IsControl) {
		return
	}
	if utf8.RuneCountInString(m.query+addition) > 256 {
		m.inputFailure = "Search is limited to 256 Unicode characters"
		return
	}
	m.inputFailure = ""
	m.query += addition
	m.ensureSelection()
}

func (m pickerModel) filtered() []SessionEntry {
	result := make([]SessionEntry, 0, len(m.entries))
	q := strings.ToLower(m.query)
	for _, e := range m.entries {
		if e.ThreadID == m.source {
			continue
		}
		if strings.Contains(strings.ToLower(string(e.Title)), q) ||
			strings.Contains(strings.ToLower(string(e.ThreadID)+" "+string(e.Family)+" "+e.Wire+" "+e.Model+" "+e.CWD), q) {
			result = append(result, e)
		}
	}
	return result
}
func (m *pickerModel) ensureSelection() {
	rows := m.filtered()
	for _, e := range rows {
		if e.ThreadID == m.selected {
			return
		}
	}
	m.selected = ""
	if len(rows) > 0 {
		m.selected = rows[0].ThreadID
	}
}
func (m *pickerModel) move(delta int) {
	rows := m.filtered()
	for i, e := range rows {
		if e.ThreadID == m.selected {
			m.selected = rows[max(0, min(len(rows)-1, i+delta))].ThreadID
			return
		}
	}
	m.ensureSelection()
}
func (m pickerModel) selection() (SessionEntry, bool) {
	for _, e := range m.filtered() {
		if e.ThreadID == m.selected {
			return e, true
		}
	}
	return SessionEntry{}, false
}
func (m pickerModel) compatible(e SessionEntry) bool {
	return e.Family == m.config.Family && e.Wire == m.config.Wire && e.Model == m.config.Model
}

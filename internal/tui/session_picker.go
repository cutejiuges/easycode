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

// StartupModel 将选择器与聊天模型组合在同一个终端事件循环中。
type StartupModel struct {
	host                       PickerHost
	config                     PickerConfig
	chat                       *Model
	request                    PickerRequest
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
	exitCode                   int
	closing                    bool
}

// NewStartupModel 只构造内存状态，不读取终端或存储。
func NewStartupModel(host PickerHost, config PickerConfig) StartupModel {
	return StartupModel{host: host, config: config, all: config.All, width: 80, height: 24}
}

// ExitCode 返回选择器退出结果，资源清理失败由宿主覆盖。
func (m StartupModel) ExitCode() int { return m.exitCode }

// Init 显式启动首次发现操作。
func (m StartupModel) Init() tea.Cmd { return func() tea.Msg { return pickerStart{} } }

type pickerStart struct{}
type pickerClosed struct{}

func waitPicker(host PickerHost) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-host.Results()
		if !ok {
			return pickerClosed{}
		}
		return result
	}
}
func (m *StartupModel) requestOperation(op PickerOperation) tea.Cmd {
	m.request = PickerRequest{ID: m.request.ID + 1, Operation: op, All: m.all, ThreadID: m.selected}
	m.busy, m.failure = true, ""
	if !m.host.Submit(m.request) {
		m.busy = false
		m.failure = "Startup is closing"
		return nil
	}
	return nil
}

// Update 只处理界面状态，存储操作由宿主串行执行。
func (m StartupModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.closing {
		return m, tea.Quit
	}
	if m.chat != nil {
		next, cmd := m.chat.Update(message)
		chat := next.(Model)
		m.chat = &chat
		return m, cmd
	}
	switch v := message.(type) {
	case pickerStart:
		m.requestOperation(PickerList)
		return m, waitPicker(m.host)
	case pickerClosed:
		if m.exitCode == 0 {
			m.exitCode = 1
		}
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		return m, nil
	case PickerResult:
		if v.Request != m.request {
			return m, waitPicker(m.host)
		}
		m.busy = false
		if v.Fatal {
			m.exitCode = 1
			m.failure = v.Error
			m.closing = true
			m.host.Stop()
			return m, tea.Quit
		}
		if v.Error != "" {
			m.failure = v.Error
			return m, waitPicker(m.host)
		}
		switch v.Request.Operation {
		case PickerList, PickerRefresh:
			m.entries, m.hasMore, m.warning, m.loaded = v.Entries, v.HasMore, v.Warning, true
			m.entriesAll = v.Request.All
			m.ensureSelection()
		case PickerPreview:
			if m.previewOpen {
				m.preview = &v.Preview
			}
		case PickerResume:
			if v.Chat != nil && m.host.Accept(v.Request.ID) {
				m.chat = v.Chat
				next, resize := m.chat.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
				chat := next.(Model)
				m.chat = &chat
				return m, tea.Batch(m.chat.Init(), resize)
			}
			m.exitCode = 130
			m.closing = true
			m.host.Stop()
			return m, tea.Quit
		}
		return m, waitPicker(m.host)
	case tea.KeyMsg:
		key := v.String()
		if key == "ctrl+c" || (key == "esc" && m.busy && m.request.Operation == PickerResume) {
			m.exitCode = 130
			m.closing = true
			m.host.Stop()
			return m, tea.Quit
		}
		if m.busy && m.request.Operation == PickerResume {
			return m, nil
		}
		if key == "esc" {
			if m.details {
				m.details = false
				m.scroll = 0
			} else if m.previewOpen {
				m.preview = nil
				m.previewOpen = false
				m.scroll = 0
				m.request.ID++
				m.host.CancelPending()
				m.busy = false
			} else if m.confirm {
				m.confirm = false
			} else if m.query != "" {
				m.query = ""
				m.inputFailure = ""
				m.ensureSelection()
			} else {
				m.closing = true
				m.host.Stop()
				return m, tea.Quit
			}
			return m, nil
		}
		if m.previewOpen || m.details {
			switch key {
			case "up":
				m.scroll = max(0, m.scroll-1)
			case "down":
				m.scroll++
			case "pgup":
				m.scroll = max(0, m.scroll-max(1, m.height-4))
			case "pgdown":
				m.scroll += max(1, m.height-4)
			}
			return m, nil
		}
		if m.confirm {
			switch key {
			case "up":
				m.scroll = max(0, m.scroll-1)
			case "down":
				m.scroll++
			case "pgup":
				m.scroll = max(0, m.scroll-max(1, m.height-2))
			case "pgdown":
				m.scroll += max(1, m.height-2)
			case "left", "right", "tab":
				m.confirmProceed = !m.confirmProceed
			case "enter":
				m.confirm = false
				if m.confirmProceed {
					m.requestOperation(PickerResume)
				}
			}
			return m, nil
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
			m.requestOperation(PickerList)
		case "ctrl+r":
			m.loaded = false
			m.requestOperation(PickerRefresh)
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
				m.requestOperation(PickerPreview)
			}
		case "enter":
			if !m.loaded || m.busy {
				break
			}
			entry, ok := m.selection()
			if !ok {
				break
			}
			if !m.compatible(entry) {
				m.failure = m.compatibilityDifference(entry)
				break
			}
			if entry.CWD != m.config.CWD {
				m.confirm = true
				m.scroll = 0
				m.confirmProceed = false
			} else {
				m.requestOperation(PickerResume)
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
			if v.Type == tea.KeyRunes {
				addition := string(v.Runes)
				if v.Paste {
					addition = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(addition)
				}
				valid := utf8.ValidString(addition)
				for _, r := range addition {
					if unicode.IsControl(r) {
						valid = false
					}
				}
				if valid && utf8.RuneCountInString(m.query+addition) > 256 {
					m.inputFailure = "Search is limited to 256 Unicode characters"
				} else if valid {
					m.inputFailure = ""
					m.query += addition
					m.ensureSelection()
				}
			}
		}
	}
	return m, nil
}
func (m StartupModel) filtered() []SessionEntry {
	result := make([]SessionEntry, 0, len(m.entries))
	q := strings.ToLower(m.query)
	for _, e := range m.entries {
		if strings.Contains(strings.ToLower(string(e.ThreadID)+" "+string(e.Family)+" "+e.Wire+" "+e.Model+" "+e.CWD), q) {
			result = append(result, e)
		}
	}
	return result
}
func (m *StartupModel) ensureSelection() {
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
func (m *StartupModel) move(delta int) {
	rows := m.filtered()
	for i, e := range rows {
		if e.ThreadID == m.selected {
			m.selected = rows[max(0, min(len(rows)-1, i+delta))].ThreadID
			return
		}
	}
	m.ensureSelection()
}
func (m StartupModel) selection() (SessionEntry, bool) {
	for _, e := range m.filtered() {
		if e.ThreadID == m.selected {
			return e, true
		}
	}
	return SessionEntry{}, false
}
func (m StartupModel) compatible(e SessionEntry) bool {
	return e.Family == m.config.Family && e.Wire == m.config.Wire && e.Model == m.config.Model
}

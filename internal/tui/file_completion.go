package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// FileQueries 只交换有界文件名快照；发现和正文准备由宿主分别拥有。
type FileQueries interface {
	QueryFiles(domain.FileSearchQuery) error
	CancelFiles(domain.FileSearchIdentity)
	FileResults() <-chan domain.FileSearchSnapshot
}

type fileResultWaiter struct {
	generation uint64
	stop       chan struct{}
	stopped    bool
}
type fileResultMsg struct {
	generation uint64
	snapshot   domain.FileSearchSnapshot
	closed     bool
}
type fileCompletionState struct {
	queries                       FileQueries
	workspace, session, request   uint64
	waiter                        *fileResultWaiter
	target                        domain.FileMentionTarget
	valid, active, ready, closed  bool
	parsed                        bool
	revision                      uint64
	cursor                        int
	identity                      domain.FileSearchIdentity
	indexRevision                 uint64
	suppression                   completionSuppression
	suppressedToken, selectedPath string
	suppressedStart               int
	paths                         []string
	selected, top, rows, visible  int
	state                         domain.FileSearchState
	report                        domain.FileDiscoveryReport
	view                          string
}

// WithFileQueries 装配工作区来源并使旧查询失效，不启动后台工作或访问文件。
func (m Model) WithFileQueries(queries FileQueries) Model {
	m.cancelFileSession()
	if m.files.waiter != nil && !m.files.waiter.stopped {
		close(m.files.waiter.stop)
		m.files.waiter.stopped = true
	}
	m.files.workspace++
	m.files.queries, m.files.closed = queries, false
	m.files.valid = false
	m.files.parsed = false
	if queries != nil && m.files.waiter == nil {
		m.files.waiter = &fileResultWaiter{generation: m.files.workspace, stop: make(chan struct{})}
	}
	return m
}

func waitFileResult(queries FileQueries, waiter *fileResultWaiter) tea.Cmd {
	if queries == nil || waiter == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case <-waiter.stop:
			return fileResultMsg{generation: waiter.generation, closed: true}
		default:
		}
		select {
		case <-waiter.stop:
			return fileResultMsg{generation: waiter.generation, closed: true}
		case result, ok := <-queries.FileResults():
			return fileResultMsg{generation: waiter.generation, snapshot: result, closed: !ok}
		}
	}
}
func (m Model) initialFileWait() tea.Cmd { return waitFileResult(m.files.queries, m.files.waiter) }

func (m Model) receiveFileResult(msg fileResultMsg) (tea.Model, tea.Cmd) {
	s := &m.files
	if s.waiter == nil || msg.generation != s.waiter.generation {
		return m, nil
	}
	s.waiter = nil
	if msg.closed && msg.generation == s.workspace {
		s.queries = nil
		m.cancelFileSession()
	}
	value := msg.snapshot
	if !msg.closed && msg.generation == s.workspace && s.active && value.Validate() == nil && value.Identity() == s.identity && value.IndexRevision() >= s.indexRevision {
		selected := s.selectedPath
		if len(s.paths) > 0 {
			selected = s.paths[s.selected]
		}
		s.paths, s.state, s.report = value.Paths(), value.State(), value.Report()
		s.indexRevision, s.ready, s.selected = value.IndexRevision(), true, 0
		for i, path := range s.paths {
			if path == selected {
				s.selected = i
				break
			}
		}
	}
	if s.queries == nil || s.closed {
		return m, nil
	}
	s.waiter = &fileResultWaiter{generation: s.workspace, stop: make(chan struct{})}
	return m, waitFileResult(s.queries, s.waiter)
}

func (m *Model) cancelFileSession() {
	s := &m.files
	if s.active && s.queries != nil {
		s.queries.CancelFiles(s.identity)
	}
	s.active, s.ready = false, false
	s.paths, s.view, s.rows = nil, "", 0
	s.visible = 0
	s.indexRevision, s.selected, s.top = 0, 0, 0
}
func (m *Model) closeFileQueries() {
	m.cancelFileSession()
	s := &m.files
	s.closed, s.queries = true, nil
	if s.waiter != nil && !s.waiter.stopped {
		close(s.waiter.stop)
		s.waiter.stopped = true
	}
}
func (m *Model) suppressPastedFiles() {
	s := &m.files
	target, ok := m.findFileMentionTarget(m.composer.cursor)
	if !ok && s.valid {
		target, ok = m.findFileMentionTarget(s.target.Start + 1)
	}
	if ok {
		s.target, s.valid = target, true
		m.suppressFiles(completionPasted)
	} else {
		m.cancelFileSession()
		s.valid = false
	}
}

func (m Model) findFileMentionTarget(cursor int) (domain.FileMentionTarget, bool) {
	head, _ := domain.SplitInputHead(m.composer.text)
	if strings.HasPrefix(head, "/") && isHostSlashName(head[1:], m.handoffEnabled, m.workMode.enabled) {
		return domain.FileMentionTarget{}, false
	}
	return domain.FindFileMentionTarget(m.composer.text, cursor)
}

func fileTargetToken(text string, target domain.FileMentionTarget) string {
	return text[target.Start:target.PathEnd]
}
func (m *Model) suppressFiles(reason completionSuppression) {
	s := &m.files
	if s.valid {
		s.suppression, s.suppressedStart = reason, s.target.Start
		s.suppressedToken = strings.Clone(fileTargetToken(m.composer.text, s.target))
	}
	m.cancelFileSession()
}

func (m *Model) prepareFiles() {
	s := &m.files
	if s.queries == nil || s.closed {
		s.view, s.rows, s.valid = "", 0, false
		return
	}
	target, valid := s.target, s.valid
	if !s.parsed || s.revision != m.composer.revision || s.cursor != m.composer.cursor {
		target, valid = m.findFileMentionTarget(m.composer.cursor)
		if valid {
			points := m.composer.layout.points
			i := sort.Search(len(points), func(i int) bool { return int(points[i].offset) >= target.Start })
			valid = i < len(points) && int(points[i].offset) == target.Start
		}
		s.parsed, s.revision, s.cursor = true, m.composer.revision, m.composer.cursor
	}
	if !valid {
		m.cancelFileSession()
		s.valid = false
		return
	}
	previous, hadTarget := s.target, s.valid
	s.target, s.valid = target, true
	if target.Start != s.suppressedStart || fileTargetToken(m.composer.text, target) != s.suppressedToken {
		s.suppression = completionNotSuppressed
	}
	if m.completionBlocked() {
		m.suppressFiles(completionModal)
		return
	}
	if s.suppression != completionNotSuppressed {
		m.cancelFileSession()
		return
	}
	if !s.active || s.identity.ComposerRevision != m.composer.revision || s.identity.Target != target {
		if !s.active || !hadTarget || previous.Start != target.Start {
			m.cancelFileSession()
			s.session++
			s.selectedPath = ""
		} else if len(s.paths) > 0 {
			s.selectedPath = s.paths[s.selected]
		}
		s.request++
		s.identity = domain.FileSearchIdentity{WorkspaceGeneration: s.workspace, SessionGeneration: s.session, ComposerRevision: m.composer.revision, RequestGeneration: s.request, Target: target}
		// 新请求先撤销旧行的接受资格，宿主只更新最新槽，不在 UI 内执行发现。
		s.active, s.ready, s.paths, s.selected, s.top = true, false, nil, 0, 0
		s.state, s.report = domain.FileSearchProgress, domain.FileDiscoveryReport{}
		query, err := domain.NewFileSearchQuery(s.identity)
		if err == nil {
			err = s.queries.QueryFiles(query)
		}
		if err != nil {
			s.ready, s.state = true, domain.FileSearchFailed
			s.report = domain.FileDiscoveryReport{Failure: domain.FileUnavailable}
		}
	}
	m.layoutFiles()
}

func (m *Model) layoutFiles() {
	s := &m.files
	s.view, s.rows, s.visible = "", 0, 0
	width, space := min(m.widthOrDefault(), maxCompletionLineBytes), m.completionSpace()
	if width < minCompletionColumns || space == 0 {
		return
	}
	status := "Searching workspace files…"
	if s.ready {
		switch s.state {
		case domain.FileSearchComplete:
			status = "No matching files"
		case domain.FileSearchIncomplete:
			status = s.report.Issues.Description()
		case domain.FileSearchFailed:
			status = "Workspace files unavailable"
		}
	}
	count := min(5, len(s.paths), max(0, space-1))
	s.visible = count
	lines := make([]string, 0, count+1)
	if count > 0 {
		s.top = min(s.top, s.selected)
		s.top = max(s.top, s.selected-count+1)
		s.top = max(0, min(s.top, len(s.paths)-count))
		for i := s.top; i < s.top+count; i++ {
			marker := "  "
			if i == s.selected {
				marker = "> "
			}
			lines = append(lines, ansi.Truncate(marker+"@"+safeCompletionText(s.paths[i])+" [file]", width, "…"))
		}
		if s.state == domain.FileSearchComplete {
			status = "Tab/Enter: fill · Esc: close"
		}
		status = fmt.Sprintf("%d/%d · %s", s.selected+1, len(s.paths), status)
	}
	lines = append(lines, ansi.Truncate(status, width, "…"))
	s.rows, s.view = len(lines), strings.Join(lines, "\n")+"\n"
}

func (m *Model) handleFileKey(key tea.KeyMsg) bool {
	s := &m.files
	if key.Paste || key.Alt || key.Type == tea.KeyCtrlJ || !s.valid {
		return false
	}
	if key.Type == tea.KeyTab && s.suppression != completionNotSuppressed && s.suppression != completionAccepted && s.suppression != completionCancelled {
		s.suppression = completionNotSuppressed
		m.prepareFiles()
		return s.rows > 0
	}
	if s.rows == 0 {
		return false
	}
	if key.Type == tea.KeyEsc {
		m.suppressFiles(completionDismissed)
		return true
	}
	if !s.ready || s.state == domain.FileSearchFailed || len(s.paths) == 0 || s.visible == 0 {
		return false
	}
	switch key.Type {
	case tea.KeyUp:
		s.selected = (s.selected + len(s.paths) - 1) % len(s.paths)
		return true
	case tea.KeyDown:
		s.selected = (s.selected + 1) % len(s.paths)
		return true
	case tea.KeyTab, tea.KeyEnter:
		if err := m.composer.completeFile(s.target, s.paths[s.selected]); err != nil {
			m.errorSummary = "invalid_input: " + err.Error()
			return true
		}
		m.clearEffortFeedback()
		m.cancelFileSession()
		if target, ok := m.findFileMentionTarget(s.target.Start + 1); ok {
			s.target = target
			m.suppressFiles(completionAccepted)
		}
		s.valid = false
		return true
	}
	return false
}

// completeFile 原子替换整个引用，保留范围及其后的分隔字素。
func (c *composer) completeFile(target domain.FileMentionTarget, path string) error {
	rendered, err := domain.RenderFilePath(path)
	if err != nil {
		return err
	}
	inserted := "@" + rendered + c.text[target.PathEnd:target.End]
	separator := false
	if target.End < len(c.text) {
		r, _ := utf8.DecodeRuneInString(c.text[target.End:])
		separator = unicode.IsSpace(r)
	}
	if !separator {
		inserted += " "
	}
	if err := c.replace(target.Start, target.End, inserted); err != nil {
		return err
	}
	if separator {
		c.horizontal(1)
	}
	return nil
}

func fileSummaryText(summary domain.FileSummary) string {
	var text strings.Builder
	for _, item := range summary.Items() {
		fmt.Fprintf(&text, "\nFile attached: %s · lines %d-%d/%d · %d bytes", item.Reference().String(), item.ActualStartLine(), item.ActualEndLine(), item.TotalLines(), item.RenderedBytes())
		if item.Truncated() {
			text.WriteString(" · truncated")
		}
		if item.EndsMidLine() {
			text.WriteString(" · partial last line")
		}
	}
	if summary.Omitted() > 0 {
		fmt.Fprintf(&text, "\n%d more file attachment(s) · Ctrl+O for details", summary.Omitted())
	}
	return text.String()
}

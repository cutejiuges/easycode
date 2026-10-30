// Package tui 实现 Bubble Tea 文本 Chat 和 RuntimeEvent 投影。
package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

// ChatSession 是 TUI 使用的最小会话接口。
type ChatSession interface {
	Outputs() <-chan protocol.ControlItem
	Append(context.Context, string) (protocol.CommandResult, error)
	Retract(context.Context, []domain.InputID) (protocol.CommandResult, error)
	Interrupt()
}

type approvalSession interface {
	ResolveApproval(context.Context, domain.TurnID, protocol.ToolApprovalRequestedPayload, protocol.ApprovalDecision, string) error
}

// RuntimeEventMsg 将 RuntimeEvent 送入 Bubble Tea update loop。
type RuntimeEventMsg struct {
	Event protocol.Event
}

type submissionFailedMsg struct{ err error }

type eventStreamClosedMsg struct{}
type approvalSubmitErrorMsg struct{ err error }

type transcriptMessage struct {
	activity *protocol.ToolActivityPayload
	preview  string
	saving   string
	role     string
	text     string
}

type turnState uint8

const (
	stateIdle turnState = iota
	stateSubmitting
	stateStreaming
)

// Model 保存纯 UI 投影状态，不依赖 Provider 或 transport。
type Model struct {
	main    mainTranscript
	browser transcriptBrowser
	version string
	width   int
	height  int
	session ChatSession
	submit  func(ChatSession, string) tea.Cmd

	composer       composer
	pending        *composer
	transcript     []transcriptMessage
	state          turnState
	assistantItem  int
	errorSummary   string
	outputs        <-chan protocol.ControlItem
	inputs         []inputPreview
	retracting     *inputRetraction
	submitContext  context.Context
	interruptNext  bool
	approval       *approvalOverlay
	activeTurnID   domain.TurnID
	toolItems      map[string]int
	toolClosed     map[string]bool
	toolActivities map[string]toolActivityState
	detailOpen     bool
	detailScroll   int
	cancelling     bool
}

type approvalOverlay struct {
	request   protocol.ToolApprovalRequestedPayload
	turnID    domain.TurnID
	selected  int
	scroll    int
	feedback  []rune
	editing   bool
	submitted bool
}

// NewModel 创建纯内存模型；恢复正文由宿主查询显式加载。
func NewModel(
	submitContext context.Context,
	version string,
	session ChatSession,
) Model {
	model := Model{
		version: version, session: session, submitContext: submitContext, assistantItem: -1, composer: newComposer(),
		submit: func(target ChatSession, prompt string) tea.Cmd {
			return submitTurn(submitContext, target, prompt)
		},
	}
	if session != nil {
		model.outputs = session.Outputs()
	}
	return model
}

// WithInputHistory 从已有语义视图初始化有界输入召回，不保留历史视图或参与 transcript 渲染。
func (model Model) WithInputHistory(history domain.SemanticHistoryView) Model {
	for _, turn := range history.Turns {
		for _, message := range turn.Messages {
			if message.Role == domain.SemanticUser {
				model.composer.history.add(message.Text)
			}
		}
	}
	return model
}

// Init 返回初始命令。
func (model Model) Init() tea.Cmd {
	return tea.Batch(waitForControl(model.outputs), model.initialTranscriptQuery())
}

// Update 更新纯 UI 状态，并把会话操作留给 tea.Cmd。
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case mainTranscriptReply:
		return model.receiveMainTranscript(value)
	case transcriptReply:
		return model.receiveTranscriptPage(value)
	case tea.WindowSizeMsg:
		model.width = value.Width
		model.height = value.Height
		model.composer.resize(value.Width, value.Height)
		return model, nil
	case protocol.ControlItem:
		next, cmd := model.projectControlItem(value)
		return next, tea.Batch(cmd, waitForControl(model.outputs))
	case eventStreamClosedMsg:
		if model.state == stateStreaming {
			model.state = stateIdle
			model.assistantItem = -1
			model.outputs = nil
			model.interruptNext = false
			model.errorSummary = "stream_protocol_error: runtime event stream closed before terminal"
		}
		return model, nil
	case RuntimeEventMsg:
		next, cmd := model.projectRuntimeEvent(value.Event)
		updated := next.(Model)
		if value.Event.Kind == protocol.EventToolTerminal && updated.detailOpen && updated.browser.queries != nil && updated.errorSummary == "" {
			refreshed, query := updated.refreshTranscriptBrowser()
			return refreshed, tea.Batch(cmd, query)
		}
		return next, cmd
	case submissionFailedMsg:
		if model.state != stateSubmitting || model.pending == nil {
			return model, nil
		}
		model.composer = *model.pending
		model.composer.resize(model.widthOrDefault(), model.heightOrDefault())
		model.pending = nil
		model.refreshState()
		model.interruptNext = false
		model.cancelling = false
		model.errorSummary = safeErrorSummary(value.err)
		return model, nil
	case retractFailedMsg:
		model.retracting = nil
		model.errorSummary = safeErrorSummary(value.err)
		return model, nil
	case approvalSubmitErrorMsg:
		if model.approval != nil {
			model.approval.submitted = false
		}
		model.errorSummary = safeErrorSummary(value.err)
		return model, nil
	case tea.KeyMsg:
		return model.handleKey(value)
	default:
		return model, nil
	}
}

func (model Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste && (model.approval != nil || model.detailOpen || (model.pending != nil || model.retracting != nil)) {
		if model.approval != nil && model.approval.editing && !model.approval.submitted {
			return model.handleApprovalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: key.Runes, Paste: true})
		}
		return model, nil
	}
	if model.approval != nil {
		return model.handleApprovalKey(key)
	}
	if key.Type == tea.KeyCtrlO {
		return model.openTranscriptBrowser()
	}
	if model.detailOpen {
		return model.handleTranscriptBrowserKey(key)
	}
	if key.Type == tea.KeyCtrlC && model.activeTurnID.Valid() {
		model.cancelling = true
		return model, interruptSession(model.session)
	}
	if model.cancelling {
		return model, nil
	}
	if model.pending != nil || model.retracting != nil {
		if model.pending != nil && (key.Type == tea.KeyCtrlC || key.Type == tea.KeyEsc) {
			if model.activeTurnID.Valid() {
				model.cancelling = true
				return model, interruptSession(model.session)
			}
			model.interruptNext = true
		}
		return model, nil
	}
	if key.Type == tea.KeyCtrlR || key.Type == tea.KeyCtrlD {
		return model.handleReturnedInput(key.Type == tea.KeyCtrlR)
	}
	if key.Type == tea.KeyEsc || (key.Type == tea.KeyUp && model.composer.text == "" && model.composer.rowIndex() == 0) {
		if model.hasQueuedInput() {
			return model.retractQueuedInputs()
		}
		if key.Type == tea.KeyEsc && model.activeTurnID.Valid() {
			model.cancelling = true
			return model, interruptSession(model.session)
		}
	}

	intent, err := model.composer.input(key)
	if err != nil {
		model.errorSummary = "invalid_input: " + err.Error()
		return model, nil
	}
	switch intent {
	case composerQuit:
		if model.activeTurnID.Valid() || model.composer.text != "" {
			return model, nil
		}
		return model, tea.Quit
	case composerSubmit:
		if model.session == nil {
			return model, nil
		}
		if !model.canRetainInput(model.composer.text) {
			model.errorSummary = "input_queue_full: take or discard returned inputs first"
			return model, nil
		}
		snapshot := model.composer
		model.pending = &snapshot
		model.state = stateSubmitting
		model.errorSummary = ""
		return model, model.submit(model.session, snapshot.text)
	}
	return model, nil
}

func (model Model) widthOrDefault() int {
	if model.width <= 0 {
		return 80
	}
	return model.width
}

func (model Model) heightOrDefault() int {
	if model.height <= 0 {
		return 24
	}
	return model.height
}

func (model Model) handleApprovalKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	overlay := model.approval
	if overlay.submitted {
		if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
			return model, interruptSession(model.session)
		}
		return model, nil
	}
	if overlay.editing {
		switch key.Type {
		case tea.KeyEsc:
			overlay.editing = false
			return model, nil
		case tea.KeyBackspace, tea.KeyDelete:
			if len(overlay.feedback) > 0 {
				overlay.feedback = overlay.feedback[:len(overlay.feedback)-1]
			}
		case tea.KeyEnter:
			return model, model.submitApproval()
		case tea.KeyCtrlC:
			overlay.submitted = true
			return model, interruptSession(model.session)
		case tea.KeySpace:
			if len(string(overlay.feedback))+1 <= 16<<10 {
				overlay.feedback = append(overlay.feedback, ' ')
			}
		case tea.KeyRunes:
			for _, character := range key.Runes {
				if len(string(overlay.feedback))+utf8.RuneLen(character) <= 16<<10 {
					overlay.feedback = append(overlay.feedback, character)
				}
			}
		}
		return model, nil
	}
	switch key.Type {
	case tea.KeyLeft, tea.KeyUp:
		if overlay.request.Capability == "process.exec" {
			overlay.selected = (overlay.selected + 2) % 3
		} else if overlay.selected > 0 {
			overlay.selected--
		}
	case tea.KeyRight, tea.KeyDown:
		if overlay.request.Capability == "process.exec" {
			overlay.selected = (overlay.selected + 1) % 3
		} else if overlay.selected < 2 {
			overlay.selected++
		}
	case tea.KeyPgUp:
		overlay.scroll--
	case tea.KeyPgDown:
		overlay.scroll++
	case tea.KeyTab:
		if overlay.request.Decisions[overlay.selected] == protocol.ApprovalDeny {
			overlay.editing = true
		}
	case tea.KeyEnter:
		return model, model.submitApproval()
	case tea.KeyEsc, tea.KeyCtrlC:
		overlay.submitted = true
		return model, interruptSession(model.session)
	}
	return model, nil
}

func (model *Model) submitApproval() tea.Cmd {
	overlay := model.approval
	if overlay == nil {
		return nil
	}
	target, ok := model.session.(approvalSession)
	if !ok {
		model.errorSummary = "approval_unavailable: interactive approval is unavailable"
		return nil
	}
	decision := overlay.request.Decisions[overlay.selected]
	feedback := ""
	if decision == protocol.ApprovalDeny {
		feedback = strings.TrimSpace(string(overlay.feedback))
	}
	overlay.submitted = true
	request := overlay.request
	return func() tea.Msg {
		if err := target.ResolveApproval(context.Background(), overlay.turnID, request, decision, feedback); err != nil {
			return approvalSubmitErrorMsg{err: err}
		}
		return nil
	}
}

func (model Model) projectRuntimeEvent(event protocol.Event) (tea.Model, tea.Cmd) {
	terminal := false
	switch event.Kind {
	case protocol.EventTranscriptCommitted:
		payload, err := protocol.DecodeTranscriptCommitted(event)
		if err != nil || event.TurnID != model.activeTurnID {
			model.errorSummary = "stream_protocol_error: invalid transcript commit"
			return model, nil
		}
		return model.commitMainTranscript(payload.Sequence)
	case protocol.EventToolReady, protocol.EventToolStarted, protocol.EventToolProgress, protocol.EventToolTerminal:
		payload, err := protocol.DecodeToolActivity(event)
		if err != nil || event.TurnID != model.activeTurnID || model.toolClosed[payload.InvocationID] {
			model.errorSummary = "stream_protocol_error: invalid command activity"
			return model, nil
		}
		if model.toolItems == nil {
			model.toolItems = make(map[string]int)
			model.toolClosed = make(map[string]bool)
		}
		index, exists := model.toolItems[payload.InvocationID]
		if !exists {
			if event.Kind != protocol.EventToolStarted && event.Kind != protocol.EventToolReady {
				model.errorSummary = "stream_protocol_error: command has not started"
				return model, nil
			}
			index = len(model.transcript)
			model.toolItems[payload.InvocationID] = index
			model.transcript = append(model.transcript, transcriptMessage{role: "Command"})
		} else if event.Kind == protocol.EventToolReady || (event.Kind == protocol.EventToolStarted && model.toolActivities[payload.InvocationID].activity.Status != "waiting") {
			model.errorSummary = "stream_protocol_error: duplicate command start"
			return model, nil
		}
		if event.Kind == protocol.EventToolTerminal {
			model.toolClosed[payload.InvocationID] = true
		}
		model.transcript[index].activity = &payload
		if event.Kind == protocol.EventToolTerminal {
			terminalPayload, decodeErr := protocol.DecodeToolTerminal(event)
			if decodeErr != nil {
				model.errorSummary = "stream_protocol_error: invalid tool terminal"
				return model, nil
			}
			model.transcript[index].preview = terminalPayload.ResultPreview
			model.transcript[index].saving = savingNotice(terminalPayload.Stdout.Summary, terminalPayload.Stderr.Summary, terminalPayload.Search.Summary)
		}
		if model.toolActivities == nil {
			model.toolActivities = make(map[string]toolActivityState)
		}
		if event.Kind == protocol.EventToolTerminal {
			delete(model.toolActivities, payload.InvocationID)
		} else {
			model.toolActivities[payload.InvocationID] = toolActivityState{activity: payload}
		}

	case protocol.EventUserInputCommitted:
		payload, err := protocol.DecodeUserInputCommitted(event)
		if err != nil || event.TurnID != model.activeTurnID {
			model.errorSummary = "stream_protocol_error: invalid committed input"
			return model, nil
		}
		model.transcript = append(model.transcript, transcriptMessage{role: "User", text: payload.Text})
		model.assistantItem = -1
	case protocol.EventTurnStarted:
		if event.TurnID.Valid() {
			model.activeTurnID = event.TurnID
			model.refreshState()
			if model.interruptNext {
				model.interruptNext = false
				model.cancelling = true
				return model, interruptSession(model.session)
			}
		}
	case protocol.EventAssistantTextDelta:
		payload, err := protocol.DecodeAssistantTextDelta(event)
		if err != nil {
			model.state = stateIdle
			model.assistantItem = -1
			model.interruptNext = false
			model.errorSummary = "stream_protocol_error: invalid assistant text event"
			return model, nil
		}
		if model.assistantItem < 0 {
			model.transcript = append(model.transcript, transcriptMessage{role: "Assistant"})
			model.assistantItem = len(model.transcript) - 1
		}
		model.transcript[model.assistantItem].text = appendProvisionalText(model.transcript[model.assistantItem].text, payload.Text)
	case protocol.EventTurnCompleted:
		if _, err := protocol.DecodeTurnCompleted(event); err != nil {
			model.state = stateIdle
			model.assistantItem = -1
			model.interruptNext = false
			model.errorSummary = "stream_protocol_error: invalid turn completion event"
			return model, nil
		}
		model.state = stateIdle
		model.assistantItem = -1
		model.interruptNext = false
		terminal = true
	case protocol.EventTurnFailed:
		if model.assistantItem >= 0 {
			model.transcript[model.assistantItem].role = "Assistant (unfinished)"
		}
		payload, err := protocol.DecodeTurnFailed(event)
		if err != nil {
			model.errorSummary = "turn_failed: invalid failure event"
		} else {
			model.errorSummary = payload.Code + ": " + payload.Message
		}
		model.state = stateIdle
		model.assistantItem = -1
		model.interruptNext = false
		terminal = true
	case protocol.EventToolApprovalRequested:
		payload, err := protocol.DecodeToolApprovalRequested(event)
		if err != nil || !event.TurnID.Valid() || model.approval != nil ||
			(model.activeTurnID != "" && model.activeTurnID != event.TurnID) {
			model.errorSummary = "stream_protocol_error: invalid approval request event"
			return model, interruptSession(model.session)
		}
		model.approval = &approvalOverlay{request: payload, turnID: event.TurnID, selected: 2}
		if payload.Capability == "process.exec" {
			model.approval.selected = 1

		}
	case protocol.EventToolApprovalResolved:
		payload, err := protocol.DecodeToolApprovalResolved(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid approval resolution event"
			return model, nil
		}
		if model.approval != nil && model.approval.request.RequestID == payload.RequestID {
			request := model.approval.request
			if event.TurnID != model.approval.turnID || payload.InvocationID != request.InvocationID || payload.Fingerprint != request.Fingerprint {
				model.errorSummary = "stream_protocol_error: approval resolution identity mismatch"
				return model, nil
			}
			model.approval = nil
		}
	}
	if terminal {
		model.approval = nil
		model.activeTurnID = ""
		model.cancelling = false
	}
	model.refreshState()
	return model, nil
}

// View 渲染确定性的聊天投影和已准备的多行输入布局。
func (model Model) View() string {
	if model.approval != nil {
		return model.renderApproval()
	}
	if model.detailOpen {
		return model.renderTranscriptBrowser()
	}
	if model.composer.height < 5 && model.state == stateIdle {
		return model.composer.view(true)
	}
	var view strings.Builder
	_, _ = fmt.Fprintf(&view, "EasyCode %s\n", model.version)
	if model.cancelling {
		view.WriteString("Cancelling — waiting for cleanup\n")
	}
	view.WriteString("\n")
	messages := model.visibleTranscript()
	lines := renderTranscript(messages, model.widthOrDefault())
	available := max(1, model.heightOrDefault()-strings.Count(model.composer.view(true), "\n")-strings.Count(model.renderInputs(), "\n")-5)
	if len(lines) > available {
		lines = lines[len(lines)-available:]
	}
	view.WriteString(strings.Join(lines, "\n"))
	if len(lines) > 0 {
		view.WriteByte('\n')
	}

	if model.errorSummary != "" {
		_, _ = fmt.Fprintf(&view, "Error: %s\n", model.errorSummary)
	}
	view.WriteString("\n")
	view.WriteString(model.renderInputs())
	view.WriteString(model.composer.view(model.pending == nil && model.retracting == nil && !model.cancelling))
	return view.String()
}

func (model Model) renderApproval() string {
	overlay := model.approval
	if overlay == nil {
		return ""
	}
	if overlay.request.Capability == "process.exec" {
		return model.renderCommandApproval()
	}
	width := model.width - 6
	if width < 20 {
		width = 20
	}
	if width > 92 {
		width = 92
	}
	contentWidth := width - 2
	height := model.height - 15
	if height < 2 {
		height = 2
	}
	lines := wrapApprovalDiff(overlay.request.Presentation.Text(), contentWidth)
	maxScroll := len(lines) - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := overlay.scroll
	if scroll < 0 {
		scroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	end := scroll + height
	if end > len(lines) {
		end = len(lines)
	}
	var output strings.Builder
	left := max(0, (model.width-width-2)/2)
	padding := strings.Repeat(" ", left)
	title := "Approve file patch"
	switch overlay.request.Presentation.Kind {
	case protocol.PresentationWriteCreate:
		title = "Create file"
	case protocol.PresentationWriteOverwrite:
		title = "Overwrite file"
	}
	output.WriteString(padding + "┌─ " + title + " " + strings.Repeat("─", max(0, width-ansi.StringWidth(title)-3)) + "┐\n")
	row := func(value string) {
		value = truncateRunes(value, contentWidth)
		fmt.Fprintf(&output, "%s│ %s%s │\n", padding, value, strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(value))))
	}
	row("Path: " + protocol.VisibleApprovalText(overlay.request.RelativePath))
	row("Risk: " + overlay.request.Reason)
	row(fmt.Sprintf("Diff %d-%d of %d", scroll+1, end, len(lines)))
	for _, line := range lines[scroll:end] {
		row(line)
	}
	choices := []string{"Yes", "Yes, allow all edits during this session", "No"}
	if overlay.request.Capability == "fs.write" {
		choices[1] = "Yes, allow creating and overwriting files this session"
	}
	for index, choice := range choices {
		marker := " "
		if index == overlay.selected {
			marker = ">"
		}
		row(marker + " " + choice)
	}
	if overlay.editing {
		feedback := "Feedback (what should EasyCode do instead?): " + string(overlay.feedback)
		row(feedback)
	} else if overlay.request.Decisions[overlay.selected] == protocol.ApprovalDeny {
		row("Tab: add feedback · Enter: deny · Esc: interrupt")
	} else {
		row("Enter: approve · Esc: interrupt")
	}
	if overlay.submitted {
		row("Submitting decision…")
	}
	output.WriteString(padding + "└" + strings.Repeat("─", width) + "┘\n")
	return output.String()
}

// commandDisplayText 仅清理终端投影，保留原始候选和持久化结果的字节语义。
func commandDisplayText(value string) string {
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' {
			return character
		}
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return '�'
		}
		return character
	}, value)
}

func wrapApprovalDiff(diff string, width int) []string {
	return strings.Split(ansi.Hardwrap(diff, max(1, width), true), "\n")
}

func truncateRunes(value string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(value, width, "…")
}

func submitTurn(ctx context.Context, session ChatSession, prompt string) tea.Cmd {
	return func() tea.Msg {
		_, err := session.Append(ctx, prompt)
		if err != nil {
			return submissionFailedMsg{err: err}
		}
		return nil
	}
}

func waitForControl(outputs <-chan protocol.ControlItem) tea.Cmd {
	if outputs == nil {
		return nil
	}
	return func() tea.Msg {
		item, open := <-outputs
		if !open {
			return eventStreamClosedMsg{}
		}
		return item
	}
}

func interruptSession(session ChatSession) tea.Cmd {
	return func() tea.Msg {
		if session != nil {
			session.Interrupt()
		}
		return nil
	}
}

func safeErrorSummary(err error) string {
	summary := fault.Project(err)
	return string(summary.Code) + ": " + summary.Message
}

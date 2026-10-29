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
	Submit(context.Context, string) (<-chan protocol.Event, error)
	Interrupt()
}

type approvalSession interface {
	ResolveApproval(context.Context, domain.TurnID, protocol.ToolApprovalRequestedPayload, protocol.ApprovalDecision, string) error
}

// RuntimeEventMsg 将 RuntimeEvent 送入 Bubble Tea update loop。
type RuntimeEventMsg struct {
	Event protocol.Event
}

type streamReadyMsg struct {
	events <-chan protocol.Event
}

type submissionFailedMsg struct{ err error }

type eventStreamClosedMsg struct{}
type approvalSubmitErrorMsg struct{ err error }

type transcriptMessage struct {
	role string
	text string
}

type turnState uint8

const (
	stateIdle turnState = iota
	stateSubmitting
	stateStreaming
)

// Model 保存纯 UI 投影状态，不依赖 Provider 或 transport。
type Model struct {
	browser commandBrowser
	version string
	width   int
	height  int
	session ChatSession
	submit  func(ChatSession, string) tea.Cmd

	composer      composer
	pending       *composer
	transcript    []transcriptMessage
	state         turnState
	assistantItem int
	errorSummary  string
	events        <-chan protocol.Event
	interruptNext bool
	approval      *approvalOverlay
	activeTurnID  domain.TurnID
	commandItems  map[string]int
	commandClosed map[string]bool
	commandViews  map[string]commandDetail
	detailOpen    bool
	detailScroll  int
	cancelling    bool
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

// NewModel 创建初始 Bubble Tea 模型，并投影可选的已恢复语义历史。
func NewModel(
	submitContext context.Context,
	version string,
	session ChatSession,
	initialHistory ...domain.SemanticHistoryView,
) Model {
	model := Model{
		version: version, session: session, assistantItem: -1, composer: newComposer(),
		submit: func(target ChatSession, prompt string) tea.Cmd {
			return submitTurn(submitContext, target, prompt)
		},
	}
	if len(initialHistory) == 0 {
		return model
	}
	for _, turn := range initialHistory[0].Turns {
		model.composer.history.add(turn.UserText)
		model.transcript = append(model.transcript,
			transcriptMessage{role: "User", text: turn.UserText},
			transcriptMessage{role: "Assistant", text: turn.AssistantText},
		)
	}
	return model
}

// Init 返回初始命令。
func (Model) Init() tea.Cmd {
	return nil
}

// Update 更新纯 UI 状态，并把会话操作留给 tea.Cmd。
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case commandListReply:
		return model.receiveCommandList(value)
	case commandPageReply:
		return model.receiveCommandPage(value)
	case tea.WindowSizeMsg:
		model.width = value.Width
		model.height = value.Height
		model.composer.resize(value.Width, value.Height)
		return model, nil
	case streamReadyMsg:
		if model.state != stateSubmitting || model.pending == nil {
			return model, nil
		}
		prompt := model.pending.text
		model.transcript = append(model.transcript, transcriptMessage{role: "User", text: prompt})
		model.composer.history.add(prompt)
		model.composer.clear()
		model.pending = nil
		model.state = stateStreaming
		model.events = value.events
		waitCommand := waitForEvent(value.events)
		if !model.interruptNext {
			return model, waitCommand
		}
		model.interruptNext = false
		return model, tea.Batch(waitCommand, interruptSession(model.session))
	case eventStreamClosedMsg:
		if model.state == stateStreaming {
			model.state = stateIdle
			model.assistantItem = -1
			model.events = nil
			model.interruptNext = false
			model.errorSummary = "stream_protocol_error: runtime event stream closed before terminal"
		}
		return model, nil
	case RuntimeEventMsg:
		next, cmd := model.projectRuntimeEvent(value.Event)
		updated := next.(Model)
		if value.Event.Kind == protocol.EventCommandTerminal && updated.detailOpen && updated.browser.queries != nil && updated.errorSummary == "" {
			refreshed, query := updated.refreshCommandBrowser()
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
		model.state = stateIdle
		model.interruptNext = false
		model.cancelling = false
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
	if key.Paste && (model.approval != nil || model.detailOpen || model.state != stateIdle) {
		if model.approval != nil && model.approval.editing && !model.approval.submitted {
			return model.handleApprovalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: key.Runes, Paste: true})
		}
		return model, nil
	}
	if model.approval != nil {
		return model.handleApprovalKey(key)
	}
	if key.Type == tea.KeyCtrlO {
		return model.openCommandBrowser()
	}
	if model.detailOpen {
		return model.handleCommandBrowserKey(key)
	}
	if model.state != stateIdle {
		if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
			model.cancelling = true
			if model.events == nil {
				model.interruptNext = true
				return model, nil
			}
			return model, interruptSession(model.session)
		}
		return model, nil
	}

	intent, err := model.composer.input(key)
	if err != nil {
		model.errorSummary = "invalid_input: " + err.Error()
		return model, nil
	}
	switch intent {
	case composerQuit:
		return model, tea.Quit
	case composerSubmit:
		if model.session == nil {
			return model, nil
		}
		snapshot := model.composer
		model.pending = &snapshot
		model.state = stateSubmitting
		model.assistantItem = -1
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
	case protocol.EventCommandStarted, protocol.EventCommandProgress, protocol.EventCommandTerminal:
		payload, err := protocol.DecodeCommandActivity(event)
		if err != nil || event.TurnID != model.activeTurnID || model.commandClosed[payload.InvocationID] {
			model.errorSummary = "stream_protocol_error: invalid command activity"
			return model, nil
		}
		if model.commandItems == nil {
			model.commandItems = make(map[string]int)
			model.commandClosed = make(map[string]bool)
		}
		index, exists := model.commandItems[payload.InvocationID]
		if !exists {
			if event.Kind != protocol.EventCommandStarted {
				model.errorSummary = "stream_protocol_error: command has not started"
				return model, nil
			}
			index = len(model.transcript)
			model.commandItems[payload.InvocationID] = index
			model.transcript = append(model.transcript, transcriptMessage{role: "Command"})
		} else if event.Kind == protocol.EventCommandStarted {
			model.errorSummary = "stream_protocol_error: duplicate command start"
			return model, nil
		}
		state := "Running"
		if event.Kind == protocol.EventCommandTerminal {
			state = payload.Status + " (" + payload.Code + ")"
			if payload.ExitKnown {
				state += fmt.Sprintf(" exit=%d", payload.ExitCode)
			}
			model.commandClosed[payload.InvocationID] = true
		}
		text := fmt.Sprintf("%s\n  %s · %s · %d ms · %d bytes", commandSummary(payload.Command, model.width), state, commandDisplayText(payload.Workdir), payload.ElapsedMillis, payload.StdoutBytes+payload.StderrBytes)
		if payload.Truncated {
			text += " · truncated"
		}
		if payload.Recent != "" {
			text += "\n" + commandDisplayText(payload.Recent)
		}
		model.transcript[index].text = text
		if model.commandViews == nil {
			model.commandViews = make(map[string]commandDetail)
		}
		if event.Kind == protocol.EventCommandTerminal {
			delete(model.commandViews, payload.InvocationID)
		} else {
			model.commandViews[payload.InvocationID] = commandDetail{activity: payload}
		}

	case protocol.EventTurnStarted:
		if event.TurnID.Valid() {
			model.activeTurnID = event.TurnID
		}
	case protocol.EventAssistantTextDelta:
		payload, err := protocol.DecodeAssistantTextDelta(event)
		if err != nil {
			model.state = stateIdle
			model.events = nil
			model.assistantItem = -1
			model.interruptNext = false
			model.errorSummary = "stream_protocol_error: invalid assistant text event"
			return model, nil
		}
		if model.assistantItem < 0 {
			model.transcript = append(model.transcript, transcriptMessage{role: "Assistant"})
			model.assistantItem = len(model.transcript) - 1
		}
		model.transcript[model.assistantItem].text += payload.Text
	case protocol.EventTurnCompleted:
		if _, err := protocol.DecodeTurnCompleted(event); err != nil {
			model.state = stateIdle
			model.events = nil
			model.assistantItem = -1
			model.interruptNext = false
			model.errorSummary = "stream_protocol_error: invalid turn completion event"
			return model, nil
		}
		model.state = stateIdle
		model.assistantItem = -1
		model.events = nil
		model.interruptNext = false
		terminal = true
	case protocol.EventTurnFailed:
		payload, err := protocol.DecodeTurnFailed(event)
		if err != nil {
			model.errorSummary = "turn_failed: invalid failure event"
		} else {
			model.errorSummary = payload.Code + ": " + payload.Message
		}
		model.state = stateIdle
		model.assistantItem = -1
		model.events = nil
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
	if terminal || model.events == nil {
		return model, nil
	}
	return model, waitForEvent(model.events)
}

// View 渲染确定性的聊天投影和已准备的多行输入布局。
func (model Model) View() string {
	if model.approval != nil {
		return model.renderApproval()
	}
	if model.detailOpen {
		return model.renderCommandBrowser()
	}
	if model.composer.height < 5 && model.state == stateIdle {
		return model.composer.view(true)
	}
	var view strings.Builder
	_, _ = fmt.Fprintf(&view, "EasyCode %s\n", model.version)
	if model.state != stateIdle && model.cancelling {
		view.WriteString("Status: cancelling — waiting for cleanup\n")
	} else if model.state == stateSubmitting {
		view.WriteString("Status: submitting\n")
	} else if model.state == stateStreaming {
		view.WriteString("Status: streaming\n")
	} else {
		view.WriteString("Status: idle\n")
	}
	view.WriteString("\n")
	for _, message := range model.transcript {
		text := message.text
		if message.role == "User" {
			text = safeComposerText(text)
		}
		_, _ = fmt.Fprintf(&view, "%s: %s\n", message.role, text)
	}
	if model.errorSummary != "" {
		_, _ = fmt.Fprintf(&view, "Error: %s\n", model.errorSummary)
	}
	view.WriteString("\n")
	if model.state == stateStreaming {
		view.WriteString("> [streaming — Esc/Ctrl+C to cancel]\n")
	} else {
		view.WriteString(model.composer.view(model.state == stateIdle))
	}
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
		events, err := session.Submit(ctx, prompt)
		if err != nil {
			return submissionFailedMsg{err: err}
		}
		return streamReadyMsg{events: events}
	}
}

func waitForEvent(events <-chan protocol.Event) tea.Cmd {
	return func() tea.Msg {
		event, open := <-events
		if !open {
			return eventStreamClosedMsg{}
		}
		return RuntimeEventMsg{Event: event}
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

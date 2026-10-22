// Package tui 实现 Bubble Tea 文本 Chat 和 RuntimeEvent 投影。
package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

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

// ErrorMsg 将异步命令错误送入 Bubble Tea update loop。
type ErrorMsg struct {
	Err error
}

type streamReadyMsg struct {
	events <-chan protocol.Event
}

type eventStreamClosedMsg struct{}
type approvalSubmitErrorMsg struct{ err error }

type transcriptMessage struct {
	role string
	text string
}

type turnState uint8

const (
	stateIdle turnState = iota
	stateStreaming
)

// Model 保存纯 UI 投影状态，不依赖 Provider 或 transport。
type Model struct {
	version string
	width   int
	height  int
	session ChatSession
	submit  func(ChatSession, string) tea.Cmd

	draft         []rune
	transcript    []transcriptMessage
	state         turnState
	assistantItem int
	errorSummary  string
	events        <-chan protocol.Event
	interruptNext bool
	approval      *approvalOverlay
	activeTurnID  domain.TurnID
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
		version: version, session: session, assistantItem: -1,
		submit: func(target ChatSession, prompt string) tea.Cmd {
			return submitTurn(submitContext, target, prompt)
		},
	}
	if len(initialHistory) == 0 {
		return model
	}
	for _, turn := range initialHistory[0].Turns {
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
	case tea.WindowSizeMsg:
		model.width = value.Width
		model.height = value.Height
		return model, nil
	case streamReadyMsg:
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
		return model.projectRuntimeEvent(value.Event)
	case ErrorMsg:
		model.state = stateIdle
		model.assistantItem = -1
		model.events = nil
		model.interruptNext = false
		model.errorSummary = safeErrorSummary(value.Err)
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
	if model.approval != nil {
		return model.handleApprovalKey(key)
	}
	if model.state == stateStreaming {
		if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
			if model.events == nil {
				model.interruptNext = true
				return model, nil
			}
			return model, interruptSession(model.session)
		}
		return model, nil
	}

	switch key.Type {
	case tea.KeyCtrlC:
		if len(model.draft) == 0 {
			return model, tea.Quit
		}
	case tea.KeyEnter:
		prompt := strings.TrimSpace(string(model.draft))
		if prompt == "" || model.session == nil {
			return model, nil
		}
		model.transcript = append(model.transcript, transcriptMessage{role: "User", text: prompt})
		model.draft = nil
		model.state = stateStreaming
		model.assistantItem = -1
		model.errorSummary = ""
		return model, model.submit(model.session, prompt)
	case tea.KeyBackspace, tea.KeyDelete:
		if len(model.draft) > 0 {
			model.draft = model.draft[:len(model.draft)-1]
		}
	case tea.KeySpace:
		model.draft = append(model.draft, ' ')
	case tea.KeyRunes:
		model.draft = append(model.draft, key.Runes...)
	}
	return model, nil
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
		if overlay.selected > 0 {
			overlay.selected--
		}
	case tea.KeyRight, tea.KeyDown:
		if overlay.selected < 2 {
			overlay.selected++
		}
	case tea.KeyPgUp:
		overlay.scroll--
	case tea.KeyPgDown:
		overlay.scroll++
	case tea.KeyTab:
		if overlay.selected == 2 {
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
	decision := protocol.ApprovalDeny
	switch overlay.selected {
	case 0:
		decision = protocol.ApprovalAllowOnce
	case 1:
		decision = protocol.ApprovalAllowSession
	}
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
	case protocol.EventToolApprovalResolved:
		payload, err := protocol.DecodeToolApprovalResolved(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid approval resolution event"
			return model, nil
		}
		if model.approval != nil && model.approval.request.RequestID == payload.RequestID {
			model.approval = nil
		}
	}
	if terminal {
		model.approval = nil
		model.activeTurnID = ""
	}
	if terminal || model.events == nil {
		return model, nil
	}
	return model, waitForEvent(model.events)
}

// View 渲染确定性的单行 Chat 界面。
func (model Model) View() string {
	if model.approval != nil {
		return model.renderApproval()
	}
	var view strings.Builder
	_, _ = fmt.Fprintf(&view, "EasyCode %s\n", model.version)
	if model.state == stateStreaming {
		view.WriteString("Status: streaming\n")
	} else {
		view.WriteString("Status: idle\n")
	}
	view.WriteString("\n")
	for _, message := range model.transcript {
		_, _ = fmt.Fprintf(&view, "%s: %s\n", message.role, message.text)
	}
	if model.errorSummary != "" {
		_, _ = fmt.Fprintf(&view, "Error: %s\n", model.errorSummary)
	}
	view.WriteString("\n")
	if model.state == stateStreaming {
		view.WriteString("> [streaming — Esc/Ctrl+C to cancel]\n")
	} else if len(model.draft) == 0 {
		view.WriteString(">\n")
	} else {
		_, _ = fmt.Fprintf(&view, "> %s\n", string(model.draft))
	}
	return view.String()
}

func (model Model) renderApproval() string {
	overlay := model.approval
	if overlay == nil {
		return ""
	}
	width := model.width - 6
	if width < 20 {
		width = 20
	}
	if width > 92 {
		width = 92
	}
	contentWidth := width - 4
	height := model.height - 15
	if height < 2 {
		height = 2
	}
	lines := wrapApprovalDiff(overlay.request.Diff, contentWidth)
	maxScroll := len(lines) - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := overlay.scroll
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
	output.WriteString(padding + "┌─ Approve file patch " + strings.Repeat("─", max(0, width-22)) + "┐\n")
	fmt.Fprintf(&output, "%s│ Path: %-*s │\n", padding, contentWidth-6, truncateRunes(overlay.request.RelativePath, contentWidth-6))
	fmt.Fprintf(&output, "%s│ Risk: %-*s │\n", padding, contentWidth-6, truncateRunes(overlay.request.Reason, contentWidth-6))
	fmt.Fprintf(&output, "%s│ Diff %d-%d of %d%s│\n", padding, scroll+1, end, len(lines), strings.Repeat(" ", max(1, contentWidth-18-len(fmt.Sprintf("%d%d%d", scroll+1, end, len(lines))))))
	for _, line := range lines[scroll:end] {
		fmt.Fprintf(&output, "%s│ %-*s │\n", padding, contentWidth, line)
	}
	choices := []string{"Yes", "Yes, allow all edits during this session", "No"}
	for index, choice := range choices {
		marker := " "
		if index == overlay.selected {
			marker = ">"
		}
		fmt.Fprintf(&output, "%s│ %s %-*s │\n", padding, marker, contentWidth-2, truncateRunes(choice, contentWidth-2))
	}
	if overlay.editing {
		feedback := "Feedback (what should EasyCode do instead?): " + string(overlay.feedback)
		fmt.Fprintf(&output, "%s│ %-*s │\n", padding, contentWidth, truncateRunes(feedback, contentWidth))
	} else if overlay.selected == 2 {
		fmt.Fprintf(&output, "%s│ %-*s │\n", padding, contentWidth, truncateRunes("Tab: add feedback · Enter: deny · Esc: interrupt", contentWidth))
	} else {
		fmt.Fprintf(&output, "%s│ %-*s │\n", padding, contentWidth, "Enter: approve · Esc: interrupt")
	}
	if overlay.submitted {
		fmt.Fprintf(&output, "%s│ %-*s │\n", padding, contentWidth, "Submitting decision…")
	}
	output.WriteString(padding + "└" + strings.Repeat("─", width) + "┘\n")
	return output.String()
}

func wrapApprovalDiff(diff string, width int) []string {
	if width < 1 {
		width = 1
	}
	var wrapped []string
	for _, line := range strings.Split(diff, "\n") {
		runes := []rune(line)
		if len(runes) == 0 {
			wrapped = append(wrapped, "")
			continue
		}
		for len(runes) > width {
			wrapped = append(wrapped, string(runes[:width]))
			runes = runes[width:]
		}
		wrapped = append(wrapped, string(runes))
	}
	return wrapped
}

func truncateRunes(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func submitTurn(ctx context.Context, session ChatSession, prompt string) tea.Cmd {
	return func() tea.Msg {
		events, err := session.Submit(ctx, prompt)
		if err != nil {
			return ErrorMsg{Err: err}
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

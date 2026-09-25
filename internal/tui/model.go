// Package tui 实现 Bubble Tea 文本 Chat 和 RuntimeEvent 投影。
package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/fault"
	"easycode/internal/protocol"
)

// ChatSession 是 TUI 使用的最小会话接口。
type ChatSession interface {
	Submit(string) (<-chan protocol.Event, error)
	Interrupt()
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

	draft         []rune
	transcript    []transcriptMessage
	state         turnState
	assistantItem int
	errorSummary  string
	events        <-chan protocol.Event
	interruptNext bool
}

// NewModel 创建初始 Bubble Tea 模型。
func NewModel(version string, session ChatSession) Model {
	return Model{version: version, session: session, assistantItem: -1}
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
	case tea.KeyMsg:
		return model.handleKey(value)
	default:
		return model, nil
	}
}

func (model Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return model, submitTurn(model.session, prompt)
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

func (model Model) projectRuntimeEvent(event protocol.Event) (tea.Model, tea.Cmd) {
	terminal := false
	switch event.Kind {
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
	}
	if terminal || model.events == nil {
		return model, nil
	}
	return model, waitForEvent(model.events)
}

// View 渲染确定性的单行 Chat 界面。
func (model Model) View() string {
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
	} else {
		_, _ = fmt.Fprintf(&view, "> %s\n", string(model.draft))
	}
	return view.String()
}

func submitTurn(session ChatSession, prompt string) tea.Cmd {
	return func() tea.Msg {
		events, err := session.Submit(prompt)
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
	if err == nil {
		return "turn_failed: turn failed"
	}
	var typed *fault.Error
	if errors.As(err, &typed) {
		return string(typed.Code) + ": " + typed.Message
	}
	return "turn_failed: turn failed"
}

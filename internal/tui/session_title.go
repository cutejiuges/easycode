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
	"easycode/internal/protocol"
)

const maxSessionTitleEditorBytes = 1 << 10

type sessionTitleHost interface {
	RenameSession(context.Context, protocol.RequestID, domain.SessionTitle) (protocol.CommandResult, error)
}

type sessionTitleView struct {
	value    domain.SessionTitle
	editor   *composer
	request  *sessionTitleRequest
	sequence uint64
	saved    bool
	closed   bool
}

type sessionTitleRequest struct {
	id     protocol.RequestID
	value  domain.SessionTitle
	cancel context.CancelFunc
	quit   bool
}

type sessionTitleDeliveryFailedMsg struct {
	id  protocol.RequestID
	err error
}

// WithSessionTitle 初始化宿主已验证的会话标题。
func (model Model) WithSessionTitle(title domain.SessionTitle) Model {
	model.title.value = title
	return model
}

// sessionTitleArguments 只切分首词，保留其后的原始分隔符和参数。
func sessionTitleArguments(text string) (string, bool) {
	arguments, matched := strings.CutPrefix(strings.TrimLeftFunc(text, unicode.IsSpace), "/rename")
	if !matched {
		return "", false
	}
	first, _ := utf8.DecodeRuneInString(arguments)
	return arguments, arguments == "" || unicode.IsSpace(first)
}

func (model *Model) suppressSessionTitleCompletion() {
	model.completion.refreshTarget(model.composer)
	model.completion.suppress(completionModal)
}

func (model Model) beginSessionTitle(arguments string) (tea.Model, tea.Cmd) {
	model.clearEffortFeedback()
	if strings.TrimFunc(arguments, func(r rune) bool { return unicode.Is(unicode.Zs, r) }) != "" {
		return model.saveSessionTitle(arguments)
	}
	if _, ok := model.session.(sessionTitleHost); !ok {
		model.errorSummary = "invalid_command: session title control is unavailable"
		return model, nil
	}
	if model.activeTurnID.Valid() || model.activeCompactionID.Valid() || model.hasQueuedInput() {
		model.errorSummary = "session_busy: session has an active operation or pending input"
		return model, nil
	}
	editor := newComposer()
	editor.resize(model.widthOrDefault(), model.heightOrDefault())
	// 装配只传入已验证标题，长度和字节均满足普通 composer 的边界。
	_ = editor.replace(0, 0, string(model.title.value))
	model.title.editor = &editor
	model.errorSummary = ""
	model.suppressSessionTitleCompletion()
	return model, nil
}

func (model Model) saveSessionTitle(text string) (tea.Model, tea.Cmd) {
	title, err := domain.ParseSessionTitle(text)
	if err != nil {
		model.errorSummary = "invalid_input: " + err.Error()
		return model, nil
	}
	target, ok := model.session.(sessionTitleHost)
	if !ok {
		model.errorSummary = "invalid_command: session title control is unavailable"
		return model, nil
	}
	model.title.sequence++
	id := protocol.RequestID(fmt.Sprintf("tui-rename-%d", model.title.sequence))
	ctx, cancel := context.WithCancel(model.submitContext)
	model.title.request = &sessionTitleRequest{id: id, value: title, cancel: cancel}
	model.errorSummary = ""
	return model, func() tea.Msg {
		_, err := target.RenameSession(ctx, id, title)
		if err != nil {
			return sessionTitleDeliveryFailedMsg{id: id, err: err}
		}
		// 已交付命令只由有序 ControlItem 呈现确定结果。
		return nil
	}
}

func (model Model) handleSessionTitleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if pending := model.title.request; pending != nil {
		if !key.Paste && key.Type == tea.KeyCtrlC && !pending.quit {
			request := *pending
			request.quit = true
			request.cancel()
			model.title.request = &request
		}
		return model, nil
	}
	if !key.Paste {
		switch key.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			model.title.editor = nil
			model.errorSummary = ""
			return model, nil
		case tea.KeyEnter:
			if !key.Alt {
				return model.saveSessionTitle(model.title.editor.text)
			}
			return model, nil
		case tea.KeyCtrlJ:
			return model, nil
		}
	}
	editor := *model.title.editor
	if key.Paste || !key.Alt && (key.Type == tea.KeyRunes || key.Type == tea.KeySpace) {
		inserted := string(key.Runes)
		if key.Type == tea.KeySpace && !key.Paste {
			inserted = " "
		}
		if len(inserted) > maxSessionTitleEditorBytes-len(editor.text) {
			model.errorSummary = "invalid_input: session title editor is limited to 1024 bytes"
			return model, nil
		}
		if err := domain.ValidateSessionTitleText(inserted); err != nil {
			model.errorSummary = "invalid_input: " + err.Error()
			return model, nil
		}
	}
	if _, err := editor.input(key); err != nil {
		model.errorSummary = "invalid_input: session title edit is invalid"
		return model, nil
	}
	model.errorSummary = ""
	model.title.editor = &editor
	return model, nil
}

func (model *Model) resizeSessionTitleEditor() {
	if model.title.editor != nil {
		editor := *model.title.editor
		editor.resize(model.widthOrDefault(), model.heightOrDefault())
		model.title.editor = &editor
	}
}

func (model Model) projectSessionTitleResult(result protocol.CommandResult) (tea.Model, tea.Cmd) {
	request := model.title.request
	if request == nil || request.id != result.RequestID() {
		return model, nil
	}
	if summary, rejected := result.Error(); rejected {
		switch summary.Code() {
		case protocol.ControlErrorSessionFailed, protocol.ControlErrorSessionClosing, protocol.ControlErrorSessionShutdown:
			model.title.closed = true
		}
		return model.finishSessionTitleRequest(string(summary.Code()) + ": " + summary.Message())
	}
	model.title.value = request.value
	model.title.saved = true
	model.title.editor = nil
	model.composer.clear()
	return model.finishSessionTitleRequest("")
}

func (model Model) finishSessionTitleRequest(failure string) (tea.Model, tea.Cmd) {
	request := model.title.request
	request.cancel()
	model.title.request = nil
	model.errorSummary = failure
	if request.quit {
		return model.quitTranscript()
	}
	return model, nil
}

func (model Model) closeSessionTitleInput(failure string) (tea.Model, tea.Cmd) {
	if model.title.closed {
		return model, nil
	}
	model.title.closed = true
	if model.title.request != nil {
		return model.finishSessionTitleRequest("session_failed: could not confirm session title save")
	}
	model.errorSummary = failure
	return model, nil
}

func (model Model) chatComposerEditable() bool {
	return model.pending == nil && model.retracting == nil && !model.cancelling && model.title.request == nil
}

func (title sessionTitleView) line() string {
	if title.value == "" {
		return ""
	}
	return "Session: " + string(title.value)
}

func (title sessionTitleView) status() string {
	if title.request != nil {
		if title.request.quit {
			return "Waiting for session title save before exiting…"
		}
		return "Saving session title…"
	}
	if title.saved {
		return "Session renamed to: " + string(title.value)
	}
	return ""
}

func (model Model) renderSessionTitleEditor() string {
	width, height := model.widthOrDefault(), model.heightOrDefault()
	editor := model.title.editor
	input := editor.inputView(model.title.request == nil)
	available := height - renderedChatRows(input)
	status := model.title.status()
	if model.errorSummary != "" {
		status = "Error: " + model.errorSummary
	}
	if available > 0 && status != "" {
		input += ansi.Truncate(status, width, "…") + "\n"
		available--
	}
	if available > 0 {
		input = ansi.Truncate("Rename session", width, "…") + "\n" + input
		available--
	}
	if available > 0 && model.title.request == nil {
		input += ansi.Truncate("Enter: save | Esc: cancel", width, "…") + "\n"
	}
	return input
}

func (model Model) renderSessionTitleClosed() string {
	lines := []string{"Error: " + model.errorSummary, "Ctrl+C: exit"}
	if line := model.title.line(); line != "" && model.heightOrDefault() > len(lines) {
		lines = append([]string{line}, lines...)
	}
	lines = lines[:min(len(lines), model.heightOrDefault())]
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, model.widthOrDefault(), "…")
	}
	return strings.Join(lines, "\n") + "\n"
}

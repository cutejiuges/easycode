package tui

import (
	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

type pickerPhase uint8

const (
	shellChat pickerPhase = iota
	shellBrowsing
	shellOpening
	shellRecovering
	shellCancelling
	shellRetiring
)

// StartupModel 在唯一终端事件循环中组合当前聊天和选择器。
type StartupModel struct {
	host                    PickerHost
	config                  PickerConfig
	chat                    *Model
	picker                  pickerModel
	phase                   pickerPhase
	request                 PickerRequest
	generation              uint64
	draft                   composer
	width, height           int
	exitCode                int
	finalNotice             string
	cancelNotice            string
	branchTarget            domain.ThreadID
	reportedBranchRequestID uint64
	closing                 bool
}

// NewStartupModel 只构造内存状态，启动选择器由 Init 发起。
func NewStartupModel(host PickerHost, config PickerConfig) StartupModel {
	return StartupModel{
		host: host, config: config, phase: shellBrowsing, width: 80, height: 24,
		picker: pickerModel{config: config, all: config.All, width: 80, height: 24},
	}
}

// NewChatShell 用同一终端外层装配已经打开的初始聊天。
func NewChatShell(host InteractivePickerHost, config PickerConfig, initial Model) StartupModel {
	model := NewStartupModel(host, config)
	model.mount(initial, false)
	return model
}

// ExitCode 返回终端界面的退出结果。
func (m StartupModel) ExitCode() int { return m.exitCode }

// ReportedBranchRequestID 返回已安装到终端投影的最近一次分支发布结果请求号。
func (m StartupModel) ReportedBranchRequestID() uint64 { return m.reportedBranchRequestID }

// Init 启动初始投影和唯一的宿主结果等待。
func (m StartupModel) Init() tea.Cmd {
	if m.chat != nil {
		return tea.Batch(waitPicker(m.host), scopeChatCommand(m.generation, m.chat.Init()))
	}
	return func() tea.Msg { return pickerStart{} }
}

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

func (m *StartupModel) requestOperation(op PickerOperation, target domain.ThreadID) bool {
	return m.submitPicker(PickerRequest{Operation: op, All: m.picker.all, Source: m.picker.source, ThreadID: target})
}

func (m *StartupModel) submitPicker(request PickerRequest) bool {
	request.ID = m.request.ID + 1
	m.request = request
	m.picker.busy, m.picker.failure = true, ""
	if m.host.Submit(m.request) {
		return true
	}
	m.picker.busy = false
	m.picker.failure = "Session chooser is closing"
	return false
}

// Update 将键盘焦点交给且仅交给当前活动界面。
func (m StartupModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.closing {
		return m, tea.Quit
	}
	switch v := message.(type) {
	case pickerStart:
		m.requestOperation(PickerList, "")
		return m, waitPicker(m.host)
	case pickerClosed:
		return m.stop(1, "Session host closed unexpectedly")
	case PickerResult:
		return m.receivePicker(v)
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, v.Width), max(1, v.Height)
		m.picker.width, m.picker.height = m.width, m.height
		if m.chat != nil {
			return m.updateChat(v)
		}
	case tea.KeyMsg:
		return m.handleKey(v)
	case scopedChatMessage:
		return m.receiveChat(v)
	}
	return m, nil
}

func (m StartupModel) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.phase == shellChat {
		return m.updateChat(key)
	}
	if !key.Paste && key.Type == tea.KeyCtrlC {
		if m.chat == nil || m.phase == shellRetiring {
			return m.stop(130, "")
		}
		m.cancelResume()
		return m, nil
	}
	if m.phase != shellBrowsing {
		if !key.Paste && key.Type == tea.KeyEsc && m.phase == shellRetiring && m.chat == nil {
			return m.stop(130, "")
		}
		if !key.Paste && key.Type == tea.KeyEsc && (m.phase == shellOpening || m.phase == shellRecovering) {
			if m.chat == nil {
				return m.stop(130, "")
			}
			m.cancelResume()
			return m, nil
		}
		return m, nil
	}
	action := m.picker.key(key)
	if action.cancelPreview {
		m.host.CancelPending()
	}
	if action.close {
		if m.chat == nil {
			return m.stop(0, "")
		}
		m.cancelResume()
		return m, nil
	}
	if action.operation != 0 {
		submitted := m.requestOperation(action.operation, m.picker.selected)
		if action.operation == PickerResume && submitted {
			m.phase = shellRecovering
		}
	}
	return m, nil
}

func (m *StartupModel) cancelResume() {
	if m.phase != shellCancelling {
		m.phase = shellCancelling
		m.host.(InteractivePickerHost).CancelResume(m.request)
	}
}

func (m StartupModel) receivePicker(result PickerResult) (tea.Model, tea.Cmd) {
	if result.Fatal {
		if result.Request.Operation == PickerBranch {
			if result.Request != m.request || m.phase == shellChat {
				return m, waitPicker(m.host)
			}
			return m.stop(1, m.branchResultNotice(result, result.Error))
		}
		return m.stop(1, result.Error)
	}
	if result.Request != m.request || m.phase == shellChat {
		return m, waitPicker(m.host)
	}
	switch result.Stage {
	case PickerCancelled:
		if m.phase == shellRetiring {
			break
		}
		if m.chat == nil {
			m.phase = shellBrowsing
			m.picker.busy, m.picker.failure = false, result.Error
			return m, waitPicker(m.host)
		}
		notice := result.Error
		if m.cancelNotice != "" {
			notice = m.cancelNotice
		}
		if result.Request.Operation == PickerBranch {
			m.returnFromBranch(result, notice)
		} else {
			m.returnToChat(notice, result.DirectoryMismatch)
		}
	case PickerPrepared:
		if m.phase != shellRecovering {
			break
		}
		if !m.validPickerCandidate(result) {
			if m.chat != nil {
				m.cancelNotice = "Initial conversation history is invalid"
				m.cancelResume()
				return m, waitPicker(m.host)
			}
			return m.stop(1, "Session host offered an invalid conversation")
		}
		if m.host.Accept(result.Request.ID) {
			m.phase = shellRetiring
			m.branchTarget = result.BranchThreadID
		} else {
			m.phase = shellCancelling
		}
	case PickerActivated:
		if result.Request.Operation == PickerBranch && m.phase != shellRetiring {
			break
		}
		if m.phase != shellRetiring || !m.validPickerCandidate(result) {
			return m.stop(1, "Session host delivered an invalid conversation")
		}
		resumed := m.chat != nil
		chat := *result.Chat
		if result.Request.Operation == PickerBranch {
			chat.branchNotice = "Branched session " + string(result.BranchThreadID) + "\nReturn to source: /resume " + string(result.Request.Source)
			m.reportedBranchRequestID = result.Request.ID
		}
		m.mount(chat, resumed)
		return m, tea.Batch(waitPicker(m.host), m.activateChat())
	case PickerCompleted:
		if m.phase == shellCancelling || m.phase == shellRetiring {
			break
		}
		m.applyPickerResult(result)
	}
	return m, waitPicker(m.host)
}

func (m StartupModel) validPickerCandidate(result PickerResult) bool {
	if result.Chat == nil || !result.Chat.main.initialReady {
		return false
	}
	target := result.Request.ThreadID
	if result.Request.Operation == PickerBranch {
		target = result.BranchThreadID
		if result.BranchPublication != BranchPublished || !target.Valid() || target == result.Request.Source ||
			m.phase == shellRetiring && target != m.branchTarget || result.Chat.title.value.Validate() != nil {
			return false
		}
	}
	return result.Chat.browser.thread == target
}

func (m *StartupModel) branchResultNotice(result PickerResult, base string) string {
	status := "Branch was not created"
	if result.BranchPublication != BranchNotPublished && (!result.BranchThreadID.Valid() || result.BranchThreadID == result.Request.Source) {
		status = "Branch publication could not be confirmed"
	} else if result.BranchPublication == BranchPublished {
		status = "Branch created. Open it with /resume " + string(result.BranchThreadID)
		m.reportedBranchRequestID = result.Request.ID
	} else if result.BranchPublication == BranchPublicationUncertain {
		status = "Branch publication is uncertain. Target thread: " + string(result.BranchThreadID)
		m.reportedBranchRequestID = result.Request.ID
	}
	if base != "" {
		status += "\n" + pickerSafe(base, 1024)
	}
	return status
}

func (m *StartupModel) applyPickerResult(result PickerResult) {
	m.picker.busy = false
	m.phase = shellBrowsing
	if result.Request.Operation == PickerPreview && !m.picker.previewOpen {
		return
	}
	if result.Error != "" {
		m.picker.failure = pickerSafe(result.Error, 1024)
		return
	}
	switch result.Request.Operation {
	case PickerOpen, PickerList, PickerRefresh:
		m.picker.entries, m.picker.hasMore, m.picker.warning, m.picker.loaded = result.Entries, result.HasMore, result.Warning, true
		m.picker.entriesAll = result.Request.All
		m.picker.ensureSelection()
	case PickerPreview:
		m.picker.preview = &result.Preview
	}
}

func (m *StartupModel) returnToChat(notice string, directoryMismatch bool) {
	if notice == "" {
		notice = "Resume cancelled"
	}
	m.chat.returnFromHandoff(m.draft, notice, directoryMismatch)
	m.finishHandoff()
}

func (m *StartupModel) returnFromBranch(result PickerResult, notice string) {
	m.chat.branchNotice = m.branchResultNotice(result, notice)
	m.chat.returnFromHandoff(m.draft, "", false)
	m.finishHandoff()
}

func (m *StartupModel) finishHandoff() {
	m.phase = shellChat
	m.cancelNotice = ""
	m.branchTarget = ""
	m.picker = pickerModel{}
	m.draft = composer{}
}

func (m StartupModel) stop(code int, notice string) (tea.Model, tea.Cmd) {
	m.exitCode, m.finalNotice, m.closing = code, notice, true
	m.host.Stop()
	return m, tea.Quit
}

func (m *StartupModel) mount(chat Model, resumed bool) {
	if _, available := m.host.(InteractivePickerHost); available {
		chat.handoffEnabled = true
		chat = chat.WithSkills(chat.skills)
	}
	chat.resumed = resumed
	m.generation++
	m.chat, m.phase = &chat, shellChat
	m.picker, m.draft = pickerModel{}, composer{}
	m.branchTarget = ""
}

func (m *StartupModel) activateChat() tea.Cmd {
	next, resize := m.chat.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	chat := next.(Model)
	m.chat = &chat
	return scopeChatCommand(m.generation, tea.Batch(m.chat.Init(), resize))
}

func (m StartupModel) updateChat(message tea.Msg) (tea.Model, tea.Cmd) {
	source := *m.chat
	source.planReview.suspendAutomatic = m.phase != shellChat
	next, cmd := source.Update(message)
	chat := next.(Model)
	intent := chat.handoffIntent
	chat.handoffIntent = nil
	m.chat = &chat
	if chat.quitRequested {
		return m.stop(0, "")
	}
	if intent != nil {
		m.draft = intent.draft
		m.picker = pickerModel{config: m.config, source: chat.browser.thread, width: m.width, height: m.height}
		m.phase = shellOpening
		if intent.operation == PickerBranch || intent.target.Valid() {
			m.phase = shellRecovering
		}
		if !m.submitPicker(PickerRequest{Operation: intent.operation, Source: chat.browser.thread, ThreadID: intent.target, Title: intent.title}) {
			if intent.operation == PickerBranch {
				m.returnFromBranch(PickerResult{Request: m.request}, "Session host is closing")
			} else {
				m.returnToChat(m.picker.failure, false)
			}
		}
	}
	return m, scopeChatCommand(m.generation, cmd)
}

type scopedChatMessage struct {
	generation uint64
	value      tea.Msg
}

// scopeChatCommand 在 Bubble Tea 展开批次之前绑定消息到发起它的聊天代次。
func scopeChatCommand(generation uint64, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		if message := cmd(); message != nil {
			return scopedChatMessage{generation: generation, value: message}
		}
		return nil
	}
}

func (m StartupModel) receiveChat(message scopedChatMessage) (tea.Model, tea.Cmd) {
	if m.chat == nil || message.generation != m.generation || m.phase == shellRetiring {
		return m, nil
	}
	switch value := message.value.(type) {
	case tea.BatchMsg:
		commands := make([]tea.Cmd, len(value))
		for i, cmd := range value {
			commands[i] = scopeChatCommand(m.generation, cmd)
		}
		return m, tea.Batch(commands...)
	case tea.QuitMsg:
		if m.phase == shellChat {
			return m.stop(0, "")
		}
	case eventStreamClosedMsg:
		if m.phase == shellChat && (m.chat.title.request != nil || m.chat.title.closed || m.chat.workMode.pending != nil || m.chat.planReview.waiting()) {
			return m.updateChat(value)
		}
		return m.stop(1, "Conversation control closed unexpectedly")
	default:
		return m.updateChat(value)
	}
	return m, nil
}

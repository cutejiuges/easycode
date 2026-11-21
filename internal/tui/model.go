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
	Append(context.Context, domain.InputDraft) (protocol.CommandResult, error)
	Retract(context.Context, []domain.InputID) (protocol.CommandResult, error)
	InterruptTurn(domain.TurnID)
}

type approvalSession interface {
	ResolveApproval(context.Context, domain.TurnID, protocol.ToolApprovalRequestedPayload, protocol.ApprovalDecision, string) error
}

// RuntimeEventMsg 将 RuntimeEvent 送入 Bubble Tea update loop。
type RuntimeEventMsg struct {
	Event protocol.Event
}

type submissionFailedMsg struct {
	err        error
	generation uint64
}

type eventStreamClosedMsg struct{}
type approvalSubmitErrorMsg struct {
	requestID string
	err       error
}

type transcriptMessage struct {
	admitted    bool
	hook        *protocol.HookActivityPayload
	unfinished  bool
	local       uint64
	sourceStart int
	cursor      protocol.TranscriptCursor
	more        bool
	excerpt     bool
	activity    *protocol.ToolActivityPayload
	preview     string
	saving      string
	role        string
	text        string
}

type turnState uint8

const (
	stateIdle turnState = iota
	stateSubmitting
	stateStreaming
)

// Model 保存纯 UI 投影状态，不依赖 Provider 或 transport。
type Model struct {
	resumeEnabled             bool
	resumeIntent              *chatResumeIntent
	resumed                   bool
	resumeDirectoryNotice     string
	resumeDirectoryScroll     int
	quitRequested             bool
	effortOperationGeneration uint64
	queryOperationGeneration  uint64
	effortGeneration          uint64
	effortFeedback            *effortFeedback
	effortSelection           *effortSelectionObservation
	effortNotice              []string
	stopCommit                *protocol.StopHookCommittedPayload
	stopStatus                string
	completion                completionState
	files                     fileCompletionState
	question                  *questionOverlay
	tasks                     taskProgressView
	title                     sessionTitleView
	workMode                  workModeView

	hooks                   hookBrowser
	hookTrust               *hookTrustOverlay
	nextTextID              uint64
	reading                 transcriptViewport
	markdown                markdownViewCache
	skills                  []domain.SkillListEntry
	skillsOpen              bool
	plugins                 pluginBrowser
	skillSelection          int
	queryGeneration         uint64
	queryRequestID          protocol.RequestID
	queryKind               protocol.CommandKind
	cachePanel              *protocol.CacheSnapshotPayload
	cacheScroll             int
	pendingContext          protocol.CommandKind
	activeCompactionID      domain.CompactionID
	compaction              compactionView
	lastAutomaticCompaction domain.CompactionScope
	compactionStatus        string
	contextPanel            *protocol.ContextSnapshotPayload
	inference               domain.InferenceSettings
	contextScroll           int
	main                    mainTranscript
	browser                 transcriptBrowser
	reasoning               activeReasoning
	version                 string
	width                   int
	height                  int
	session                 ChatSession
	pendingCancel           context.CancelFunc
	submissionGeneration    uint64

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
	preInputs      map[string]protocol.HookActivityPayload
	postHooks      map[string]protocol.PostHookCommittedPayload
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
	_, workModeEnabled := session.(workModeSession)
	catalog, _ := newCompletionCatalog(nil, false, workModeEnabled)
	model := Model{
		completion: completionState{catalog: catalog},
		markdown:   newMarkdownViewCache(),
		workMode:   workModeView{mode: domain.WorkModeDefault, enabled: workModeEnabled},
		version:    version, session: session, submitContext: submitContext, assistantItem: -1, composer: newComposer(),
	}
	if session != nil {
		model.outputs = session.Outputs()
		_, model.tasks.enabled = session.(taskProgressSession)
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
	return tea.Batch(waitForControl(model.outputs), model.initialTranscriptQuery(), model.initialTaskQuery(), model.initialFileWait())
}

// Update 更新纯 UI 状态，并把会话操作留给 tea.Cmd。
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := model.update(message)
	updated := next.(Model)
	updated.prepareCompletion()
	updated.prepareMarkdownView()
	updated.prepareMarkdownDetail()
	if _, resize := message.(tea.WindowSizeMsg); !resize {
		hydration := updated.scheduleHydration()
		cmd = tea.Batch(cmd, hydration)
		if updated.main.awaitingProjection && !updated.main.hydrating {
			updated.main.awaitingProjection = false
			updated.main.handoff = nil
			updated.prepareMarkdownView()
			cmd = tea.Batch(cmd, updated.scheduleHydration())
		}
	}
	return updated, cmd
}

func (model Model) update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case fileResultMsg:
		return model.receiveFileResult(value)
	case initialTaskQueryMsg:
		return model.queryTasks(false)
	case taskQueryFailedMsg:
		model.failTaskQuery(value.generation, "task_query_unavailable: task progress query failed")
		return model, nil
	case hookTrustFailedMsg:
		if model.hookTrust != nil && model.hookTrust.request.RequestID == value.request && model.hookTrust.turn == value.turn &&
			model.hookTrust.request.Target == value.target && model.compaction.stage != compactionCancelling {
			model.hookTrust.submitted = false
			model.errorSummary = safeErrorSummary(value.err)
		}
		return model, nil
	case compactionCancelFailedMsg:
		if model.activeCompactionID == value.id && model.compaction.stage == compactionCancelling {
			model.errorSummary = value.summary
		}
		return model, nil
	case hookResetMsg:
		if !model.hooks.open || model.hooks.generation != value.generation {
			return model, nil
		}
		if value.err != nil {
			model.errorSummary = safeErrorSummary(value.err)
		} else {
			model.hooks.entries = append([]domain.HookListEntry(nil), value.entries...)
		}
		return model, nil

	case transcriptHydration:
		return model.receiveHydration(value)
	case mainTranscriptReply:
		return model.receiveMainTranscript(value)
	case transcriptReply:
		return model.receiveTranscriptPage(value)
	case tea.WindowSizeMsg:
		taskAnchor := model.taskPanelAnchor()
		model.browser.markdown.preserveAnchor = true
		model.width = value.Width
		model.height = value.Height
		model.contextScroll = min(model.contextScroll, model.contextScrollLimit())
		model.composer.resize(value.Width, value.Height)
		model.restoreTaskPanelAnchor(taskAnchor)
		if model.question != nil {
			model.question.resize(value.Width, value.Height)
		}
		model.resizeSessionTitleEditor()
		return model, nil
	case protocol.ControlItem:
		next, cmd := model.projectControlItem(value)
		return next, tea.Batch(cmd, waitForControl(model.outputs))
	case eventStreamClosedMsg:
		model.outputs = nil
		failure := "session_failed: session control stream closed"
		if model.workMode.pending != nil {
			model.finishWorkModeRequest(failure)
		}
		if model.tasks.query != nil {
			model.failTaskQuery(model.tasks.query.generation, "task_query_unavailable: runtime event stream closed")
		}
		if model.activeCompactionID.Valid() {
			model.compactionStatus = ""
			model.cancelling = false
		}
		model.clearCompaction()
		model.pendingContext = ""
		if model.state == stateStreaming {
			model.state = stateIdle
			model.assistantItem = -1
			model.interruptNext = false
			failure = "stream_protocol_error: runtime event stream closed before terminal"
			model.errorSummary = failure
		}
		if _, supportsTitle := model.session.(sessionTitleHost); supportsTitle {
			return model.closeSessionTitleInput(failure)
		}
		return model, nil
	case sessionTitleDeliveryFailedMsg:
		if model.title.request == nil || model.title.request.id != value.id {
			return model, nil
		}
		return model.finishSessionTitleRequest(safeErrorSummary(value.err))
	case workModeDeliveryFailedMsg:
		if model.workMode.pending != nil && model.workMode.pending.id == value.id {
			model.finishWorkModeRequest(safeErrorSummary(value.err))
		}
		return model, nil
	case RuntimeEventMsg:
		return model.reduceRuntimeEvent(value.Event)
	case submissionFailedMsg:
		return model.failSubmission(value)
	case retractFailedMsg:
		model.retracting = nil
		model.errorSummary = safeErrorSummary(value.err)
		return model, nil
	case questionSubmitResult:
		if model.question != nil && model.question.request.RequestID == value.requestID && value.err != nil {
			model.question.submitted = false
			model.question.errorText = safeErrorSummary(value.err)
		}
		return model, nil
	case approvalSubmitErrorMsg:
		if model.approval == nil || model.approval.request.RequestID != value.requestID || model.cancelling {
			return model, nil
		}
		model.approval.submitted = false
		model.errorSummary = safeErrorSummary(value.err)
		return model, nil
	case effortFailedMsg:
		if model.effortFeedback == nil || value.generation != model.effortFeedback.generation {
			return model, nil
		}
		model.effortFeedback = nil
		model.errorSummary = safeErrorSummary(value.err)
		return model, nil
	case cacheQueryFailedMsg:
		if value.generation != model.queryGeneration {
			return model, nil
		}
		return model.failSubmission(submissionFailedMsg{err: value.err})
	case tea.KeyMsg:
		return model.handleKey(value)
	default:
		return model, nil
	}
}

func (model Model) failSubmission(message submissionFailedMsg) (tea.Model, tea.Cmd) {
	if message.generation != 0 && message.generation != model.submissionGeneration {
		return model, nil
	}
	model.pendingCancel = nil
	if model.state != stateSubmitting || model.pending == nil {
		return model, nil
	}
	model.restoreComposer(*model.pending)
	model.pending = nil
	model.pendingContext = ""
	model.refreshState()
	model.interruptNext = false
	if !model.activeTurnID.Valid() && !model.activeCompactionID.Valid() {
		model.cancelling = false
	}
	model.errorSummary = safeErrorSummary(message.err)
	return model, nil
}

func (model Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste {
		if len(key.Runes) == 0 {
			return model, nil
		}
		key.Type, key.Alt = tea.KeyRunes, false
	}
	model.title.saved = false
	focus := model.focus()
	switch focus {
	case focusClosed:
		if key.Type == tea.KeyCtrlC {
			return model.quitTranscript()
		}
		return model, nil
	case focusResumeNotice:
		model.handleResumeDirectoryKey(key)
		return model, nil
	}
	if key.Type == tea.KeyCtrlC {
		model.completion.suppress(completionCancelled)
		model.suppressFiles(completionCancelled)
	}
	if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
		model.clearEffortFeedback()
	}
	switch focus {
	case focusQuestion:
		return model.handleQuestionKey(key)
	case focusHookTrust:
		return model.handleHookTrustKey(key)
	case focusApproval:
		return model.handleApprovalKey(key)
	case focusHooks:
		return model.handleHooksKey(key)
	case focusSkills:
		return model.handleSkillsKey(key)
	}
	if (key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC) && (model.pendingContext == protocol.CommandQueryCache || model.pendingContext == protocol.CommandQueryContext) {
		if model.pending != nil {
			model.restoreComposer(*model.pending)
		}
		model.pending = nil
		model.pendingContext = ""
		model.queryRequestID = ""
		model.queryKind = ""
		model.queryGeneration++
		model.refreshState()
		if key.Type == tea.KeyEsc {
			return model, nil
		}
	}
	switch focus {
	case focusCache, focusContext:
		return model.handleContextPanelKey(key)
	case focusPlugins:
		return model.handlePluginsKey(key)
	case focusTasks:
		return model.handleTasksKey(key)
	case focusTitle:
		return model.handleSessionTitleKey(key)
	}
	if focus == focusComposer && model.workMode.pending != nil {
		if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
			if model.activeTurnID.Valid() || model.activeCompactionID.Valid() {
				return model.handleCancelKey()
			}
			model.workMode.pending.cancel()
		}
		return model, nil
	}
	if key.Type == tea.KeyCtrlO {
		model.clearEffortFeedback()
		return model.openTranscriptBrowser()
	}
	if focus == focusTranscript {
		if key.Paste {
			return model, nil
		}
		return model.handleTranscriptBrowserKey(key)
	}
	if key.Type == tea.KeyPgUp || key.Type == tea.KeyPgDown {
		model.clearEffortFeedback()
		return model.navigateTranscript(key)
	}
	if key.Type == tea.KeyCtrlC {
		return model.handleCancelKey()
	}
	return model.handleComposerKey(key)
}

func (model Model) handleCancelKey() (tea.Model, tea.Cmd) {
	if model.activeCompactionID.Valid() {
		return model.requestCompactionCancellation()
	}
	if model.activeTurnID.Valid() {
		return model.requestTurnCancellation(model.activeTurnID)
	}
	if model.cancelling {
		return model, nil
	}
	if model.pending != nil {
		if model.pendingCancel != nil {
			model.pendingCancel()
		}
		model.interruptNext = true
		return model, nil
	}
	if model.retracting == nil && model.composer.text == "" {
		return model.quitTranscript()
	}
	return model, nil
}

func (model Model) requestTurnCancellation(turnID domain.TurnID) (tea.Model, tea.Cmd) {
	if model.cancelling {
		return model, nil
	}
	if model.pendingCancel != nil {
		model.pendingCancel()
	}
	model.cancelling = true
	return model, interruptSession(model.session, turnID)
}

func (model Model) handleComposerKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	newlineStart, newline := model.composer.newlineStart(key)
	submit := !key.Paste && key.Type == tea.KeyEnter && !newline
	var head, remainder string
	if submit {
		head, remainder = domain.SplitInputHead(model.composer.text)
		if model.resumeEnabled && head == "/"+resumeCommandName {
			return model.submitResume(strings.TrimSpace(remainder))
		}
	}
	if model.cancelling {
		return model, nil
	}
	if model.pending != nil || model.retracting != nil {
		if model.pending != nil && key.Type == tea.KeyEsc {
			if model.activeTurnID.Valid() {
				return model.requestTurnCancellation(model.activeTurnID)
			}
			if model.pendingCancel != nil {
				model.pendingCancel()
			}
			model.interruptNext = true
		}
		return model, nil
	}
	if newline {
		if err := model.composer.replace(newlineStart, model.composer.cursor, "\n"); err != nil {
			model.errorSummary = "invalid_input: " + err.Error()
		} else {
			model.clearEffortFeedback()
		}
		return model, nil
	}
	model.prepareCompletion()
	if key.Type == tea.KeyShiftTab && model.workMode.enabled && model.completion.rows == 0 && model.files.rows == 0 {
		return model.switchWorkMode()
	}
	if key.Type == tea.KeyCtrlT {
		if snapshot := model.tasks.snapshot; snapshot != nil && snapshot.Counts.Total > 0 {
			model.tasks.expanded = !model.tasks.expanded
		}
		return model, nil
	}
	if submit {
		if head == "/"+planCommandName && model.workMode.enabled {
			model.completion.suppress(completionCancelled)
			return model.submitPlanCommand(remainder)
		}
		switch head {
		case "/rename", "/effort", "/tasks":
			model.completion.suppress(completionCancelled)
			return model.submitComposer(head, remainder)
		}
	}
	originalDraft := model.composer
	if model.handleFileKey(key) {
		return model, nil
	}
	if handled, submit := model.handleCompletionKey(key); handled {
		if model.composer.text != originalDraft.text || model.composer.cursor != originalDraft.cursor {
			model.clearEffortFeedback()
		}
		if submit {
			if model.composer.text != originalDraft.text {
				head, remainder = domain.SplitInputHead(model.composer.text)
			}
			if head == "/"+planCommandName && model.workMode.enabled {
				model.restoreComposer(originalDraft)
				return model.submitPlanCommand(remainder)
			}
			next, command := model.submitComposer(head, remainder)
			updated := next.(Model)
			if updated.skillsOpen || updated.hooks.open || updated.plugins.open {
				updated.restoreComposer(originalDraft)
			}
			if updated.tasks.open {
				updated.tasks.draft = originalDraft
				updated.restoreComposer(originalDraft)
			}
			if updated.title.editor != nil {
				updated.restoreComposer(originalDraft)
				updated.suppressSessionTitleCompletion()
			}
			if updated.resumeIntent != nil {
				updated.resumeIntent.draft = originalDraft
			}
			return updated, command
		}
		return model, nil
	}
	if key.Type == tea.KeyCtrlR || key.Type == tea.KeyCtrlD {
		model.clearEffortFeedback()
		return model.handleReturnedInput(key.Type == tea.KeyCtrlR)
	}
	if key.Type == tea.KeyEsc || (key.Type == tea.KeyUp && model.composer.text == "" && model.composer.rowIndex() == 0) {
		if model.hasQueuedInput() {
			model.clearEffortFeedback()
			return model.retractQueuedInputs()
		}
		if key.Type == tea.KeyEsc && model.activeCompactionID.Valid() {
			return model.requestCompactionCancellation()
		}
		if key.Type == tea.KeyEsc && model.activeTurnID.Valid() {
			return model.requestTurnCancellation(model.activeTurnID)
		}
	}

	beforeText, beforeCursor := model.composer.text, model.composer.cursor
	intent, err := model.composer.input(key)
	if model.composer.text != beforeText || model.composer.cursor != beforeCursor {
		model.clearEffortFeedback()
	}
	if err != nil {
		model.errorSummary = "invalid_input: " + err.Error()
		return model, nil
	}
	if key.Paste {
		model.completion.suppressPastedInput(model.composer)
		model.suppressPastedFiles()
	}
	if intent == composerSubmit {
		return model.submitComposer(head, remainder)
	}
	return model, nil
}

func (model Model) submitComposer(head, remainder string) (tea.Model, tea.Cmd) {
	if head == "/rename" {
		return model.beginSessionTitle(remainder)
	}
	if model.resumeEnabled && head == "/"+resumeCommandName {
		return model.submitResume(strings.TrimSpace(remainder))
	}
	model.clearEffortFeedback()
	switch head {
	case "/effort":
		command, err := parseEffortArgument(remainder)
		if err != nil {
			model.errorSummary = "invalid_command: " + err.Error()
			return model, nil
		}
		return model.submitEffortCommand(command)
	case "/tasks", "/plugins", "/hooks", "/skills":
		if strings.TrimSpace(remainder) != "" {
			model.errorSummary = "invalid_command: " + head + " does not accept arguments"
			return model, nil
		}
		switch head {
		case "/tasks":
			return model.queryTasks(true)
		case "/plugins":
			model.plugins.open, model.plugins.detail = true, false
			model.plugins.selected, model.plugins.skill = 0, 0
		case "/hooks":
			return model.openHooks()
		case "/skills":
			model.skillsOpen = true
			model.skillSelection = 0
		}
		return model, nil
	}
	if model.session == nil {
		return model, nil
	}
	switch head {
	case "/cache", "/context", "/compact":
		command, err := parseContextCommand(head, remainder)
		if err != nil {
			model.errorSummary = "invalid_command: " + err.Error()
			return model, nil
		}
		return model.submitContextCommand(command)
	}
	if !model.canRetainInput(model.composer.text) {
		model.errorSummary = "input_queue_full: take or discard returned inputs first"
		return model, nil
	}
	draft, err := domain.NewInteractiveInputDraft(model.composer.text)
	if err != nil {
		model.errorSummary = "invalid_input: " + err.Error()
		return model, nil
	}
	model.cancelFileSession()
	snapshot := model.composer
	model.pending = &snapshot
	model.state = stateSubmitting
	model.errorSummary = ""
	ctx, cancel := context.WithCancel(model.submitContext)
	model.pendingCancel = cancel
	model.submissionGeneration++
	generation := model.submissionGeneration
	target := model.session
	return model, func() tea.Msg {
		defer cancel()
		_, err := target.Append(ctx, draft)
		if err != nil {
			return submissionFailedMsg{err: err, generation: generation}
		}
		return nil
	}
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
	if model.cancelling || (key.Paste && (!overlay.editing || overlay.submitted)) {
		return model, nil
	}
	if !key.Paste && (key.Type == tea.KeyCtrlC || (key.Type == tea.KeyEsc && (!overlay.editing || overlay.submitted))) {
		overlay.submitted = true
		return model.requestTurnCancellation(overlay.turnID)
	}
	if overlay.submitted || model.rewriteApprovalTooSmall() && key.Type != tea.KeyEsc {
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
			return approvalSubmitErrorMsg{requestID: request.RequestID, err: err}
		}
		return nil
	}
}

func (model Model) reduceRuntimeEvent(event protocol.Event) (tea.Model, tea.Cmd) {
	if protocol.IsCompactionPresentation(event) {
		return model.projectCompactionPresentation(event)
	}
	switch event.Kind {
	case protocol.EventTaskProgressUpdated:
		return model.projectTaskUpdate(event)
	case protocol.EventTurnStarted, protocol.EventTurnCompleted, protocol.EventTurnFailed:
		model.effortOperationGeneration++
	case protocol.EventUserQuestionRequested, protocol.EventToolApprovalRequested, protocol.EventHookTrustRequested:
		model.clearEffortFeedback()
	}
	next, cmd := model.projectRuntimeEvent(event)
	updated := next.(Model)
	if event.Kind == protocol.EventToolTerminal && updated.detailOpen && updated.browser.queries != nil && updated.errorSummary == "" {
		refreshed, query := updated.refreshTranscriptBrowser()
		return refreshed, tea.Batch(cmd, query)
	}
	return next, cmd
}

func (model Model) projectRuntimeEvent(event protocol.Event) (tea.Model, tea.Cmd) {
	if event.Kind == protocol.EventHookTrustRequested || event.Kind == protocol.EventHookTrustResolved || event.Kind == protocol.EventHookActivity || event.Kind == protocol.EventPostHookCommitted || event.Kind == protocol.EventStopHookCommitted {
		return model.projectHookEvent(event)
	}

	terminal := false
	switch event.Kind {
	case protocol.EventTranscriptCommitted:
		payload, err := protocol.DecodeTranscriptCommitted(event)
		if err != nil || event.TurnID != model.activeTurnID {
			model.errorSummary = "stream_protocol_error: invalid transcript commit"
			return model, nil
		}
		updated, detailCmd, commitErr := model.commitReasoning(payload)
		if commitErr != nil {
			model.errorSummary = "stream_protocol_error: invalid reasoning commit"
			return model, nil
		}
		next, mainCmd := updated.commitMainTranscript(payload.Sequence)
		return next, tea.Batch(detailCmd, mainCmd)
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
		if event.Kind == protocol.EventToolStarted {
			model.transcript[index].admitted = true
		}
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
		text := payload.Text
		if payload.Skill != nil {
			text += "\nSkill loaded: " + payload.Skill.Name + " (" + payload.Skill.Source.ID() + ")"
		}
		text += fileSummaryText(payload.Files)
		model.transcript = append(model.transcript, transcriptMessage{role: "User", text: text})
		model.assistantItem = -1
	case protocol.EventTurnStarted:
		model.stopCommit = nil
		model.stopStatus = ""
		model.preInputs = make(map[string]protocol.HookActivityPayload)
		model.postHooks = make(map[string]protocol.PostHookCommittedPayload)
		if event.TurnID.Valid() {
			notice := ""
			if model.reasoning.failed {
				switch model.reasoning.disposition {
				case protocol.SampleNotCommitted:
					notice = "Previous unsaved reasoning excerpt released"
				case protocol.SampleUncertain:
					notice = "Previous uncertain reasoning excerpt released"
				case protocol.SampleDurable:
					notice = "Previous saved reasoning preview released"
				}
			}
			model.reasoning = activeReasoning{notice: notice}
			if model.detailOpen {
				model.browser.invalidate()
				model.browser.detail = false
				model.browser.page = nil
				model.browser.markdown = transcriptMarkdownDetail{}
				model.browser.items = nil
			}
			model.activeTurnID = event.TurnID
			model.refreshState()
			if model.interruptNext {
				model.interruptNext = false
				return model.requestTurnCancellation(event.TurnID)
			}
		}
	case protocol.EventAssistantReasoning:
		if err := model.acceptReasoning(event); err != nil {
			model.errorSummary = "stream_protocol_error: invalid reasoning event"
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
			model.nextTextID++
			model.transcript = append(model.transcript, transcriptMessage{role: "Assistant", local: model.nextTextID})
			model.assistantItem = len(model.transcript) - 1
		}
		item := &model.transcript[model.assistantItem]
		item.excerpt = item.excerpt || len(item.text)+len(payload.Text) > 64<<10
		total := len(item.text) + len(payload.Text)
		item.text = appendProvisionalText(item.text, payload.Text)
		item.sourceStart += total - len(item.text)
	case protocol.EventTurnCompleted:
		model.stopStatus = ""
		if model.reasoning.stream.Pending() {
			model.errorSummary = "stream_protocol_error: unresolved reasoning sample"
			return model, nil
		}
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
		model.stopStatus = ""
		if model.assistantItem >= 0 {
			model.transcript[model.assistantItem].unfinished = true
		}
		payload, err := protocol.DecodeTurnFailed(event)
		if err != nil {
			model.errorSummary = "turn_failed: invalid failure event"
		} else {
			model.errorSummary = payload.Code + ": " + payload.Message
			if err := model.resolveReasoning(payload); err != nil {
				model.errorSummary = "stream_protocol_error: invalid reasoning resolution"
			}
		}
		model.state = stateIdle
		model.assistantItem = -1
		model.interruptNext = false
		terminal = true
	case protocol.EventUserQuestionRequested:
		p, err := protocol.DecodeUserQuestionRequested(event)
		if err != nil || !event.TurnID.Valid() || model.question != nil || model.approval != nil || model.hookTrust != nil || (model.activeTurnID != "" && model.activeTurnID != event.TurnID) {
			model.errorSummary = "stream_protocol_error: invalid question request"
			return model, interruptSession(model.session, model.activeTurnID)
		}
		model.question = newQuestionOverlay(p, event.TurnID, model.widthOrDefault(), model.heightOrDefault())
	case protocol.EventUserQuestionResolved:
		p, err := protocol.DecodeUserQuestionResolved(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid question resolution"
			return model, nil
		}
		if model.question != nil && model.question.request.RequestID == p.RequestID {
			q := model.question
			if q.turnID != event.TurnID || q.request.InvocationID != p.InvocationID || q.request.Fingerprint != p.Fingerprint {
				model.errorSummary = "stream_protocol_error: question resolution mismatch"
				return model, nil
			}
			model.question = nil
		}
	case protocol.EventToolApprovalRequested:
		payload, err := protocol.DecodeToolApprovalRequested(event)
		if err != nil || !event.TurnID.Valid() || model.approval != nil || model.question != nil || model.hookTrust != nil ||
			(model.activeTurnID != "" && model.activeTurnID != event.TurnID) {
			model.errorSummary = "stream_protocol_error: invalid approval request event"
			return model, interruptSession(model.session, model.activeTurnID)
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
		model.question = nil

		model.hookTrust = nil
		model.approval = nil
		model.activeTurnID = ""
		model.cancelling = false
	}
	model.refreshState()
	return model, nil
}

// View 渲染确定性的聊天投影和已准备的多行输入布局。
func (model Model) View() string {
	switch model.focus() {
	case focusClosed:
		return model.renderSessionTitleClosed()
	case focusResumeNotice:
		return model.modalWorkModeView(pickerWrapped(model.resumeDirectoryText(), model.widthOrDefault(), model.heightOrDefault(), model.resumeDirectoryScroll))
	case focusQuestion:
		return model.modalWorkModeView(model.renderQuestion())
	case focusHookTrust:
		return model.modalWorkModeView(model.renderHookTrust())
	case focusApproval:
		return model.modalWorkModeView(model.renderApproval())
	case focusHooks:
		return model.modalWorkModeView(model.renderHooks())
	case focusSkills:
		return model.modalWorkModeView(model.renderSkills())
	case focusCache:
		return model.modalWorkModeView(model.renderCachePanel())
	case focusContext:
		return model.modalWorkModeView(model.renderContextPanel())
	case focusPlugins:
		return model.modalWorkModeView(model.renderPlugins())
	case focusTasks:
		return model.modalWorkModeView(model.renderTasksPanel())
	case focusTranscript:
		return model.modalWorkModeView(model.renderTranscriptBrowser())
	case focusTitle:
		if model.title.editor != nil {
			return model.modalWorkModeView(model.renderSessionTitleEditor())
		}
	}
	if model.composer.height <= 5 {
		status := model.title.status()
		if status == "" && model.errorSummary != "" {
			if head, _ := domain.SplitInputHead(model.composer.text); head == "/rename" || model.workMode.enabled {
				status = "Error: " + model.errorSummary
			}
		}
		if status != "" {
			return model.fitChatView(status, model.composerWorkModeView(model.composer.inputView(model.chatComposerEditable())))
		}
		footer := model.composerWorkModeView(model.composer.view(model.chatComposerEditable()))
		if prefix := model.resumeStatus() + model.title.line(); prefix != "" {
			return model.fitChatView(prefix, footer)
		}
		return footer
	}
	var view strings.Builder
	_, _ = fmt.Fprintf(&view, "EasyCode %s\n", model.version)
	view.WriteString(model.resumeStatus())
	if line := model.title.line(); line != "" {
		view.WriteString(ansi.Truncate(line, model.widthOrDefault(), "…") + "\n")
	}
	if status := model.title.status(); status != "" {
		view.WriteString(ansi.Truncate(status, model.widthOrDefault(), "…") + "\n")
	}
	if model.cancelling {
		view.WriteString("Cancelling — waiting for cleanup\n")
	}
	if model.stopStatus != "" {
		view.WriteString(ansi.Truncate(model.stopStatus, model.widthOrDefault(), "…") + "\n")
	}
	if model.compactionStatus != "" {
		view.WriteString(ansi.Hardwrap(model.compactionStatus, model.widthOrDefault(), true))
		view.WriteByte('\n')
	}
	for _, line := range model.effortNoticeLines() {
		view.WriteString(line)
		view.WriteByte('\n')
	}
	if model.reasoning.notice != "" {
		view.WriteString(ansi.Truncate(model.reasoning.notice, model.widthOrDefault(), "…") + "\n")
	}
	if status := model.reasoningStatus(); status != "" {
		view.WriteString(ansi.Truncate(status, model.widthOrDefault(), "…") + "\n")
	}
	view.WriteString("\n")
	lines := model.markdown.lines
	available := model.transcriptHeight()
	start := max(0, len(lines)-available)
	if model.reading.detached {
		start = min(model.reading.scroll, max(0, len(lines)-available))
	}
	lines = lines[start:min(len(lines), start+available)]
	if model.reading.notice != "" {
		view.WriteString(ansi.Truncate(model.reading.notice, model.widthOrDefault(), "…") + "\n")
	} else if model.reading.newContent {
		view.WriteString(ansi.Truncate("New content · PgDn to return to live output", model.widthOrDefault(), "…") + "\n")
	} else if model.reading.detached {
		view.WriteString(ansi.Truncate("Reading history · PgDn to return to live output", model.widthOrDefault(), "…") + "\n")
	}
	view.WriteString(strings.Join(lines, "\n"))
	if len(lines) > 0 {
		view.WriteByte('\n')
	}

	if model.errorSummary != "" {
		_, _ = fmt.Fprintf(&view, "Error: %s\n", model.errorSummary)
	}
	view.WriteString("\n")
	footer := model.renderTaskInline() + model.renderInputs() + model.completion.view + model.files.view + model.composerWorkModeView(model.composer.view(model.chatComposerEditable()))
	return model.fitChatView(view.String(), footer)
}

func (model Model) renderApproval() string {
	overlay := model.approval
	if overlay == nil {
		return ""
	}
	if model.rewriteApprovalTooSmall() {
		return hookPanel([]string{"Approval disabled: resize terminal to at least 32×18.", "Esc: cancel. Full effective input review is required."}, model.widthOrDefault(), model.heightOrDefault(), 0)
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
	if overlay.request.InputRewrite.Kind() == protocol.PreInputSelected {
		lines = append(wrapApprovalDiff(overlay.request.Reason, contentWidth), lines...)
	}
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

func interruptSession(session ChatSession, turnID domain.TurnID) tea.Cmd {
	return func() tea.Msg {
		if session != nil {
			session.InterruptTurn(turnID)
		}
		return nil
	}
}

func safeErrorSummary(err error) string {
	summary := fault.Project(err)
	return string(summary.Code) + ": " + summary.Message
}

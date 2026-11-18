package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	taskTestSession domain.SessionID = "01900000-0000-7000-8000-000000000001"
	taskTestThread  domain.ThreadID  = "01900000-0000-7000-8000-000000000002"
	taskTestTurn    domain.TurnID    = "01900000-0000-7000-8000-000000000003"
	taskTestCall                     = "01900000-0000-7000-8000-000000000004"
)

type taskFixtureSession struct {
	fakeChatSession
	requests []protocol.RequestID
	queryErr error
}

func (session *taskFixtureSession) QueryTasks(_ context.Context, id protocol.RequestID) (protocol.CommandResult, error) {
	session.requests = append(session.requests, id)
	if session.queryErr != nil {
		return protocol.CommandResult{}, session.queryErr
	}
	command, err := protocol.NewQueryTasksCommand(id)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionQueried)
}

func taskTestUpdate(t testing.TB, steps []domain.TaskStep, explanation string) domain.TaskUpdate {
	t.Helper()
	update, err := domain.NewTaskUpdate(steps, explanation)
	if err != nil {
		t.Fatal(err)
	}
	return update
}

func taskTestStep(t testing.TB, content, active string, status domain.TaskStatus) domain.TaskStep {
	t.Helper()
	step, err := domain.NewTaskStep(content, active, status)
	if err != nil {
		t.Fatal(err)
	}
	return step
}

func taskTestState(t testing.TB, sequence uint64, count, active int, completed bool) domain.TaskProgressState {
	t.Helper()
	steps := make([]domain.TaskStep, count)
	for i := range steps {
		status := domain.TaskPending
		if completed {
			status = domain.TaskCompleted
		} else if i == active {
			status = domain.TaskInProgress
		}
		steps[i] = taskTestStep(t, fmt.Sprintf("Task %02d", i+1), fmt.Sprintf("Doing task %02d", i+1), status)
	}
	return taskTestSourceState(t, sequence, taskTestTurn, taskTestCall, taskTestUpdate(t, steps, "Model report; tests remain separate."))
}

func taskTestSourceState(t testing.TB, sequence uint64, turn domain.TurnID, invocation string, update domain.TaskUpdate) domain.TaskProgressState {
	t.Helper()
	source, err := domain.NewTaskProgressSource(sequence, turn, invocation)
	if err != nil {
		t.Fatal(err)
	}
	state, err := domain.NewTaskProgressState(source, update)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func taskTestModel(t testing.TB) Model {
	t.Helper()
	return NewModel(context.Background(), "test", &taskFixtureSession{}).WithTaskProgressScope(taskTestSession, taskTestThread)
}

func taskTestEvent(t testing.TB, state domain.TaskProgressState) protocol.Event {
	t.Helper()
	source, _ := state.Source()
	event, err := protocol.NewTaskProgressUpdated(taskTestSession, taskTestThread, source.TurnID(), state)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func taskTestObserve(t testing.TB, model Model, state domain.TaskProgressState) Model {
	t.Helper()
	next, _ := model.Update(RuntimeEventMsg{Event: taskTestEvent(t, state)})
	return next.(Model)
}

func taskTestQuery(t testing.TB, model Model, open bool) (Model, tea.Cmd) {
	t.Helper()
	next, command := model.queryTasks(open)
	return next.(Model), command
}

func taskTestAck(t testing.TB, model Model) Model {
	t.Helper()
	command, err := protocol.NewQueryTasksCommand(model.tasks.query.requestID)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionQueried)
	item, _ := protocol.NewCommandResultItem(result)
	next, _ := model.projectControlItem(item)
	return next.(Model)
}

func taskTestSnapshot(t testing.TB, model Model, requestID protocol.RequestID, state domain.TaskProgressState) Model {
	t.Helper()
	event, err := protocol.NewTaskProgressSnapshot(taskTestSession, taskTestThread, state)
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewCorrelatedTaskProgressItem(requestID, event)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := model.projectControlItem(item)
	return next.(Model)
}

func TestTasksHydrationDoesNotInventAnEmptyList(t *testing.T) {
	m := taskTestModel(t)
	if m.tasks.snapshot != nil || m.tasks.hydration != taskHydrationPending || !strings.Contains(m.View(), "Tasks: loading") {
		t.Fatal("unknown projection was presented as empty")
	}
	if m.initialTaskQuery() == nil || m.tasks.query != nil || len(m.session.(*taskFixtureSession).requests) != 0 {
		t.Fatal("constructor or Init helper performed a query")
	}
	m, command := updateModelWithCommand(t, m, initialTaskQueryMsg{})
	if command == nil || m.tasks.query == nil || !strings.Contains(m.View(), "loading") {
		t.Fatal("initial query did not remain pending")
	}
	unsafeFailure := errors.New("secret body \x1b[2J")
	session := m.session.(*taskFixtureSession)
	session.queryErr = unsafeFailure
	message := command()
	m = updateModel(t, m, message)
	if m.tasks.hydration != taskHydrationFailed || m.tasks.snapshot != nil || !strings.Contains(m.View(), "unavailable") || strings.Contains(m.View(), "secret") {
		t.Fatal("query failure leaked content or became business none")
	}
	if strings.Contains(m.renderTasksPanel(), "No task update") || !strings.Contains(m.renderTasksPanel(), "task_query_unavailable") {
		t.Fatal("failed hydration masqueraded as a valid empty state")
	}
	session.queryErr = nil
	m, command = taskTestQuery(t, m, true)
	if command() != nil || len(session.requests) != 2 {
		t.Fatal("retry did not submit one query")
	}
	m = taskTestAck(t, m)
	id := m.tasks.query.requestID
	m = taskTestSnapshot(t, m, id, domain.NewNoTaskProgress())
	if m.tasks.hydration != taskHydrationReady || m.tasks.snapshot.Progress.Kind() != domain.TaskProgressNone || !strings.Contains(m.View(), "No task update has been saved") {
		t.Fatal("validated none not accepted")
	}
	if len(session.submits) != 0 || len(m.inputs) != 0 || len(m.composer.history.entries) != 0 {
		t.Fatal("read-only query created conversation input")
	}
	cancelled := taskTestModel(t)
	cancelled, _ = taskTestQuery(t, cancelled, true)
	cancelled = taskTestAck(t, cancelled)
	closedID := cancelled.tasks.query.requestID
	cancelled = updateModel(t, cancelled, tea.KeyMsg{Type: tea.KeyEsc})
	cancelled = taskTestSnapshot(t, cancelled, closedID, domain.NewNoTaskProgress())
	if cancelled.tasks.open || cancelled.tasks.query != nil || cancelled.tasks.snapshot != nil || cancelled.tasks.hydration != taskHydrationFailed || !strings.Contains(cancelled.View(), "unavailable") {
		t.Fatal("closing first hydration left a loading state without a query")
	}
}

func TestTasksInitialStateAndDurableUpdateExpansion(t *testing.T) {
	for _, completed := range []bool{false, true} {
		m := taskTestModel(t)
		m, _ = taskTestQuery(t, m, false)
		m = taskTestAck(t, m)
		m = taskTestSnapshot(t, m, m.tasks.query.requestID, taskTestState(t, 7, 3, 1, completed))
		if m.tasks.expanded == completed {
			t.Fatal("initial expansion ignored completion")
		}
	}
	m := taskTestModel(t)
	if err := m.composer.replace(0, 0, "draft 👩‍💻"); err != nil {
		t.Fatal(err)
	}
	m.composer.moveTo(3)
	draft := m.composer
	active := taskTestState(t, 10, 20, 17, false)
	m = taskTestObserve(t, m, active)
	if !m.tasks.expanded || !strings.Contains(m.renderTaskInline(), "Doing task 18") || !strings.Contains(m.renderTaskInline(), "14 more") {
		t.Fatal("current task not anchored in the bounded window", m.renderTaskInline())
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.tasks.expanded || renderedChatRows(m.renderTaskInline()) != 1 || !reflect.DeepEqual(m.composer, draft) {
		t.Fatal("toggle changed the draft or failed to fold")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = taskTestObserve(t, m, active)
	if m.tasks.expanded {
		t.Fatal("resize or duplicate withdrew the manual fold")
	}
	m = taskTestObserve(t, m, taskTestState(t, 11, 20, -1, true))
	if m.tasks.expanded || !strings.Contains(m.renderTaskInline(), "20/20 done · model report") || m.activeTurnID != "" || m.approval != nil {
		t.Fatal("completion changed turn or permission semantics")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.tasks.expanded {
		t.Fatal("completed tasks could not be inspected")
	}
	m = taskTestObserve(t, m, taskTestState(t, 12, 0, -1, false))
	before := m.tasks.snapshot.Clone()
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.tasks.expanded || m.renderTaskInline() != "" || !m.tasks.snapshot.Equal(before) || !strings.Contains(m.renderTasksPanel(), "explicitly cleared") {
		t.Fatal("clear was lost or an empty toggle mutated state")
	}
	if len(m.session.(*taskFixtureSession).requests) != 0 || len(m.session.(*taskFixtureSession).submits) != 0 {
		t.Fatal("toggle performed a query or model submission")
	}
}

func TestTasksIgnoreStaleSnapshotsAndIsolateScope(t *testing.T) {
	m := taskTestModel(t)
	m, _ = taskTestQuery(t, m, false)
	m = taskTestAck(t, m)
	initialID := m.tasks.query.requestID
	foreignSnapshot, err := protocol.NewTaskProgressSnapshot(taskTestSession, "01900000-0000-7000-8000-000000000099", domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	foreignItem, err := protocol.NewCorrelatedTaskProgressItem(initialID, foreignSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.projectControlItem(foreignItem)
	m = next.(Model)
	if m.tasks.query == nil || m.tasks.snapshot != nil {
		t.Fatal("foreign query reply consumed the current hydration")
	}
	current := taskTestState(t, 20, 3, 1, false)
	m = taskTestObserve(t, m, current)
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	m = taskTestSnapshot(t, m, initialID, domain.NewNoTaskProgress())
	if !m.tasks.snapshot.Progress.Equal(current) || m.tasks.expanded || m.tasks.hydration != taskHydrationReady {
		t.Fatal("initial none replaced an update or manual fold")
	}
	m, _ = taskTestQuery(t, m, true)
	m = taskTestAck(t, m)
	closedID := m.tasks.query.requestID
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = taskTestObserve(t, m, taskTestState(t, 21, 4, 2, false))
	m = taskTestSnapshot(t, m, closedID, current)
	if m.tasks.open || taskResultSequence(*m.tasks.snapshot) != 21 || !m.tasks.expanded {
		t.Fatal("closed generation blocked updates or reopened the panel")
	}
	foreign := taskTestEvent(t, taskTestState(t, 22, 2, 0, false))
	foreign.ThreadID = "01900000-0000-7000-8000-000000000099"
	m = updateModel(t, m, RuntimeEventMsg{Event: foreign})
	if taskResultSequence(*m.tasks.snapshot) != 21 {
		t.Fatal("foreign thread replaced the validated projection")
	}
	first := taskTestModel(t)
	first = updateModel(t, first, RuntimeEventMsg{Event: foreign})
	if first.tasks.snapshot != nil || first.tasks.hydration != taskHydrationPending {
		t.Fatal("foreign event poisoned first hydration")
	}
	m, _ = taskTestQuery(t, m, false)
	m = taskTestAck(t, m)
	m = taskTestSnapshot(t, m, m.tasks.query.requestID, taskTestState(t, 24, 6, 4, false))
	if taskResultSequence(*m.tasks.snapshot) != 24 {
		t.Fatal("query could not recover a missed event")
	}
	m, _ = taskTestQuery(t, m, false)
	generation := m.tasks.generation
	m.failTaskQuery(generation, "task_query_unavailable: task progress query failed")
	if m.tasks.hydration != taskHydrationFailed || taskResultSequence(*m.tasks.snapshot) != 24 || !strings.Contains(m.renderTaskInline(), "unavailable") {
		t.Fatal("refresh failure discarded the validated list")
	}
	m = m.WithTaskProgressScope(taskTestSession, domain.ThreadID(foreign.ThreadID))
	if m.tasks.snapshot != nil || m.tasks.query != nil || m.tasks.hydration != taskHydrationPending || m.tasks.generation <= generation {
		t.Fatal("thread switch retained previous UI facts")
	}
}

func TestTasksSameSequenceRequiresEveryFieldToMatch(t *testing.T) {
	base := taskTestState(t, 30, 3, 1, false)
	update, _ := base.Update()
	for _, field := range []string{"turn", "invocation", "content", "active_form", "status", "order", "explanation", "counts"} {
		t.Run(field, func(t *testing.T) {
			m := taskTestObserve(t, taskTestModel(t), base)
			m.tasks.expanded, m.tasks.open, m.tasks.scroll = false, true, 1
			before := m.tasks.snapshot.Clone()
			steps, explanation := update.Items(), update.Explanation()
			turn, invocation := taskTestTurn, taskTestCall
			switch field {
			case "turn":
				turn = "01900000-0000-7000-8000-000000000005"
			case "invocation":
				invocation = "01900000-0000-7000-8000-000000000006"
			case "content":
				steps[0] = taskTestStep(t, "Changed private label", steps[0].ActiveForm(), steps[0].Status())
			case "active_form":
				steps[0] = taskTestStep(t, steps[0].Content(), "Changed active form", steps[0].Status())
			case "status":
				steps[0] = taskTestStep(t, steps[0].Content(), steps[0].ActiveForm(), domain.TaskCompleted)
			case "order":
				steps[0], steps[2] = steps[2], steps[0]
			case "explanation":
				explanation = "Changed private explanation"
			}
			state := taskTestSourceState(t, 30, turn, invocation, taskTestUpdate(t, steps, explanation))
			payload, _ := protocol.NewTaskProgressSnapshotPayload(state)
			if field == "counts" {
				payload.Counts.Pending++
			}
			if m.observeTaskProgress(taskTestEvent(t, state), payload) || !m.tasks.snapshot.Equal(before) || m.tasks.expanded || !m.tasks.open || m.tasks.scroll != 1 || !strings.Contains(m.errorSummary, "stream_protocol_error") || strings.Contains(m.errorSummary, "private") {
				t.Fatal("conflicting snapshot changed state or disclosed task text")
			}
		})
	}
	m := taskTestObserve(t, taskTestModel(t), base)
	m.tasks.expanded, m.tasks.open, m.tasks.scroll = false, true, 1
	m = taskTestObserve(t, m, base)
	if m.tasks.expanded || !m.tasks.open || m.tasks.scroll != 1 || m.errorSummary != "" {
		t.Fatal("exact duplicate changed presentation")
	}
}

func TestTasksCommandCompletionAndReadOnlyPanel(t *testing.T) {
	for _, text := range []string{"/tasks unexpected", "/tasks\nextra", "/tasks --help"} {
		m := taskTestModel(t)
		m.composer = composerWithText(t, text)
		m.composer.moveTo(3)
		before := m.composer
		m, command := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if command != nil || !reflect.DeepEqual(m.composer, before) || m.tasks.open || !strings.Contains(m.errorSummary, "invalid_command") || len(m.session.(*taskFixtureSession).submits) != 0 {
			t.Fatal("invalid local command did not preserve the full draft")
		}
	}
	for _, text := range []string{"/Tasks", "/tasks-file", "prefix /tasks", "$tasks"} {
		if local, _ := parseTasksCommand(text); local {
			t.Fatal("nonexact command intercepted", text)
		}
	}
	m := taskTestObserve(t, taskTestModel(t), taskTestState(t, 40, 20, 17, false))
	m.composer = composerWithText(t, "/ta")
	m.reading.detached = true
	m.reading.anchor = transcriptAnchor{key: markdownViewKey{local: 8}, source: 4}
	m.reading.anchors = []transcriptAnchor{m.reading.anchor}
	before := m.composer
	m, command := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || !m.tasks.open || !reflect.DeepEqual(before, m.composer) {
		t.Fatal("completion failed to open the panel without replacing its saved draft")
	}
	command()
	if len(m.session.(*taskFixtureSession).requests) != 1 || len(m.session.(*taskFixtureSession).submits) != 0 || len(m.composer.history.entries) != 0 {
		t.Fatal("panel command did not issue exactly one read-only query")
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyCtrlT}, {Type: tea.KeyRunes, Runes: []rune("secret")}, {Type: tea.KeyRunes, Runes: []rune("/tasks\x03"), Paste: true}} {
		m = updateModel(t, m, key)
		if !reflect.DeepEqual(before, m.composer) || !m.tasks.open {
			t.Fatal("modal input leaked to the composer")
		}
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.tasks.scroll <= 0 {
		t.Fatal("complete list was not scrollable")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 40, Height: 12})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.tasks.open || m.composer.text != before.text || m.composer.cursor != before.cursor || !m.reading.detached || m.reading.anchor.key.local != 8 {
		t.Fatal("closing the panel lost the draft or reading anchor")
	}
	entries := make([]domain.SkillListEntry, domain.MaxSkills)
	for i := range entries {
		entries[i] = completionSkillEntry(t, fmt.Sprintf("skill-%03d", i), "skill")
	}
	entries[0] = completionSkillEntry(t, "tasks", "same name skill")
	m = taskTestModel(t).WithSkills(entries)
	if len(m.completion.catalog.entries) != domain.MaxSkills+8 {
		t.Fatal("eighth builtin reduced the skill capacity")
	}
	m.composer = composerWithText(t, "$ta")
	m, command = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || m.composer.text != "$tasks " || m.tasks.open {
		t.Fatal("explicit skill invocation was captured by local tasks")
	}
	m.skillsOpen, m.skillSelection = true, 0
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.skillsOpen || m.composer.text != "$tasks " {
		t.Fatal("skill browser failed the reserved tasks name")
	}
	m = taskTestModel(t)
	m.composer = composerWithText(t, "/ta")
	m, command = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if command != nil || m.composer.text != "/tasks " || m.tasks.open {
		t.Fatal("Tab executed a read-only command")
	}
}

func TestTasksPanelPreservesLabelAcrossReorderResizeAndClear(t *testing.T) {
	m := taskTestObserve(t, taskTestModel(t), taskTestState(t, 50, 20, 17, false))
	m.tasks.open, m.height, m.width = true, 12, 40
	m.tasks.scroll = 17
	anchor := m.taskPanelAnchor()
	if anchor == "" {
		t.Fatal("test did not select a task label")
	}
	update, _ := m.tasks.snapshot.Progress.Update()
	steps := update.Items()
	steps[0], steps[8] = steps[8], steps[0]
	m = taskTestObserve(t, m, taskTestSourceState(t, 51, taskTestTurn, taskTestCall, taskTestUpdate(t, steps, update.Explanation())))
	if m.taskPanelAnchor() != anchor {
		t.Fatal("reordering lost the surviving label anchor")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 13, Height: 6})
	if m.taskPanelAnchor() != anchor {
		t.Fatal("resize lost the surviving label anchor")
	}
	m = taskTestObserve(t, m, taskTestState(t, 52, 2, 1, false))
	if m.tasks.scroll > m.taskPanelScrollLimit() {
		t.Fatal("removed label left scroll outside the new list")
	}
	m = taskTestObserve(t, m, taskTestState(t, 53, 0, -1, false))
	if !m.tasks.open || !strings.Contains(m.taskPanelRows()[0].text, "The task list") || m.tasks.snapshot.Progress.Kind() != domain.TaskProgressClear || m.tasks.scroll > m.taskPanelScrollLimit() {
		t.Fatal("clear closed the panel or retained old tasks")
	}
}

func TestTasksToolCandidatesAndTurnsCannotPublishProgress(t *testing.T) {
	m := taskTestObserve(t, taskTestModel(t), taskTestState(t, 100, 3, 1, false))
	m.activeTurnID, m.state = taskTestTurn, stateStreaming
	before := m.tasks.snapshot.Clone()
	activity := protocol.ToolActivityPayload{Tool: protocol.ToolTaskProgress, InvocationID: taskTestCall, Summary: "Candidate replacement", Status: "waiting"}
	ready, err := protocol.NewToolReady(activity)
	if err != nil {
		t.Fatal(err)
	}
	activity.Status = "running"
	started, err := protocol.NewToolStarted(activity)
	if err != nil {
		t.Fatal(err)
	}
	activity.Status, activity.Code = "error", "invalid_input"
	terminal, err := protocol.NewToolTerminal(protocol.ToolTerminalPayload{ToolActivityPayload: activity, ResultPreview: "Candidate was rejected"})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []protocol.Event{ready, started, terminal} {
		event.TurnID = taskTestTurn
		m = updateModel(t, m, RuntimeEventMsg{Event: event})
		if !m.tasks.snapshot.Equal(before) || m.errorSummary != "" {
			t.Fatal("candidate or failed result changed durable progress", m.errorSummary)
		}
	}
	completed := mustRuntimeCompleted(t)
	completed.TurnID = taskTestTurn
	m = updateModel(t, m, RuntimeEventMsg{Event: completed})
	startedTurn := protocol.NewTurnStarted()
	startedTurn.TurnID = "01900000-0000-7000-8000-000000000005"
	m = updateModel(t, m, RuntimeEventMsg{Event: startedTurn})
	if !m.tasks.snapshot.Equal(before) {
		t.Fatal("new turn cleared the committed list")
	}
}

type taskBrowserQueries struct{ browserQueries }

func (queries *taskBrowserQueries) ListTranscript(context.Context, protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	queries.lists++
	return protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{{Cursor: protocol.TranscriptCursor{Sequence: 12}, Kind: domain.TranscriptCall,
		TurnID: taskTestTurn, Activity: protocol.ToolActivityPayload{Tool: protocol.ToolTaskProgress, InvocationID: taskTestCall, Summary: "Update task progress", Status: "success", Code: "ok"}}}}, nil
}

func (*taskBrowserQueries) ReadTranscriptBody(context.Context, protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	const frozen = "Frozen input: original task label\nFrozen result: completed original task label"
	return protocol.TranscriptBodyPage{Text: frozen, TotalBytes: uint64(len(frozen)), NextOffset: uint64(len(frozen))}, nil
}

func TestTasksCurrentListCannotOverwriteFrozenTranscript(t *testing.T) {
	queries := &taskBrowserQueries{}
	m := taskTestObserve(t, taskTestModel(t), taskTestState(t, 110, 3, 1, false)).WithTranscriptQueries(taskTestThread, queries)
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.composer = composerWithText(t, "kept draft")
	m, command := browserKey(t, m, tea.KeyCtrlO)
	if command == nil {
		t.Fatal("history query missing")
	}
	m = updateModel(t, m, command())
	m, command = browserKey(t, m, tea.KeyEnter)
	if command == nil {
		t.Fatal("frozen body query missing")
	}
	m = updateModel(t, m, command())
	if !strings.Contains(m.View(), "original task label") || strings.Contains(m.View(), "Task 01") {
		t.Fatal("history detail substituted the current task list", m.View(), m.browser.errorText, m.browser.detail)
	}
	m = taskTestObserve(t, m, taskTestState(t, 111, 20, 17, false))
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if !m.detailOpen || !strings.Contains(m.View(), "original task label") || strings.Contains(m.View(), "Doing task 18") || m.composer.text != "kept draft" || queries.lists != 1 {
		t.Fatal("current update or toggle changed a frozen historical detail")
	}
}

func TestTasksInterruptReadAndSearchGrouping(t *testing.T) {
	activity := func(name protocol.ToolName, target, summary string) transcriptMessage {
		return transcriptMessage{activity: &protocol.ToolActivityPayload{Tool: name, Target: target, Summary: summary, Status: "success", Code: "ok"}}
	}
	read := activity(protocol.ToolRead, "a.go", "a.go")
	glob := activity(protocol.ToolGlob, "*.go", "*.go")
	grep := activity(protocol.ToolGrep, "needle", "needle")
	task := activity(protocol.ToolTaskProgress, "", "Update task progress")
	rendered := strings.Join(renderTranscript([]transcriptMessage{read, glob, grep, task, read, glob, grep}, 80), "\n")
	if strings.Count(rendered, "Read 1 files · 2 searches") != 2 || strings.Count(rendered, "TaskProgress(Update task progress)") != 1 {
		t.Fatal("task update merged independent read/search groups", rendered)
	}
	before, after, found := strings.Cut(rendered, "TaskProgress(Update task progress)")
	if !found || !strings.Contains(before, "Read 1 files") || !strings.Contains(after, "Read 1 files") {
		t.Fatal("task invocation lost its original transcript position")
	}
}

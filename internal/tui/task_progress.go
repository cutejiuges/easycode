package tui

import (
	"context"
	"fmt"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type taskProgressSession interface {
	QueryTasks(context.Context, protocol.RequestID) (protocol.CommandResult, error)
}

type taskHydration uint8

const (
	taskHydrationPending taskHydration = iota
	taskHydrationFailed
	taskHydrationReady
)

type taskProgressQuery struct {
	requestID  protocol.RequestID
	generation uint64
	accepted   bool
}

// taskProgressView 只保存当前有界投影，查询焦点与业务结果顺序分别维护。
type taskProgressView struct {
	enabled    bool
	hydration  taskHydration
	snapshot   *protocol.TaskProgressSnapshotPayload
	sessionID  domain.SessionID
	threadID   domain.ThreadID
	expanded   bool
	open       bool
	scroll     int
	draft      composer
	reading    transcriptViewport
	generation uint64
	query      *taskProgressQuery
	errorText  string
}

type initialTaskQueryMsg struct{}
type taskQueryFailedMsg struct{ generation uint64 }

// WithTaskProgressScope 绑定宿主的权威线程，切换时丢弃旧投影并使旧查询失效。
func (model Model) WithTaskProgressScope(sessionID domain.SessionID, threadID domain.ThreadID) Model {
	if model.tasks.sessionID == sessionID && model.tasks.threadID == threadID {
		return model
	}
	if model.tasks.open {
		model = model.closeTaskPanel()
	}
	generation := model.tasks.generation + 1
	enabled := model.tasks.enabled
	model.tasks = taskProgressView{enabled: enabled, generation: generation, sessionID: sessionID, threadID: threadID}
	return model
}

func parseTasksCommand(text string) (bool, error) {
	words := strings.Fields(text)
	if len(words) == 0 || words[0] != "/tasks" {
		return false, nil
	}
	if len(words) != 1 {
		return true, fmt.Errorf("/tasks does not accept arguments")
	}
	return true, nil
}

func (model Model) initialTaskQuery() tea.Cmd {
	if !model.tasks.enabled {
		return nil
	}
	return func() tea.Msg { return initialTaskQueryMsg{} }
}

func (model Model) queryTasks(open bool) (tea.Model, tea.Cmd) {
	if open && !model.tasks.open {
		model.tasks.draft = model.composer
		model.tasks.reading = model.reading
		model.tasks.open, model.tasks.scroll = true, 0
	}
	model.tasks.enabled = true
	model.tasks.generation++
	generation := model.tasks.generation
	requestID := protocol.RequestID(fmt.Sprintf("tui-tasks-%d", generation))
	model.tasks.query = &taskProgressQuery{requestID: requestID, generation: generation}
	model.tasks.errorText = ""
	if model.tasks.snapshot == nil {
		model.tasks.hydration = taskHydrationPending
	}
	target, ok := model.session.(taskProgressSession)
	if !ok {
		model.failTaskQuery(generation, "task_query_unavailable: task progress query is unavailable")
		return model, nil
	}
	ctx := model.submitContext
	return model, func() tea.Msg {
		if _, err := target.QueryTasks(ctx, requestID); err != nil {
			return taskQueryFailedMsg{generation: generation}
		}
		// 真正回执和快照统一由现有输出 owner 交付。
		return nil
	}
}

func (model *Model) failTaskQuery(generation uint64, message string) {
	if model.tasks.query == nil || model.tasks.query.generation != generation {
		return
	}
	model.tasks.query = nil
	model.tasks.hydration = taskHydrationFailed
	model.tasks.errorText = message
}

func (model Model) projectTaskQueryResult(result protocol.CommandResult) (tea.Model, tea.Cmd) {
	query := model.tasks.query
	if query == nil || query.requestID != result.RequestID() || query.generation != model.tasks.generation || query.accepted {
		return model, nil
	}
	if result.Status() == protocol.CommandStatusRejected {
		summary, _ := result.Error()
		model.failTaskQuery(query.generation, string(summary.Code())+": "+summary.Message())
		return model, nil
	}
	owned := *query
	owned.accepted = true
	model.tasks.query = &owned
	return model, nil
}

func taskResultSequence(payload protocol.TaskProgressSnapshotPayload) uint64 {
	if source, ok := payload.Progress.Source(); ok {
		return source.ResultSequence()
	}
	return 0
}

func (model Model) taskScopeMatches(event protocol.Event) bool {
	return model.tasks.threadID == "" || (model.tasks.threadID == event.ThreadID && model.tasks.sessionID == event.SessionID)
}

func (model *Model) observeTaskProgress(event protocol.Event, payload protocol.TaskProgressSnapshotPayload) bool {
	if payload.Validate() != nil {
		model.errorSummary = "stream_protocol_error: invalid task progress snapshot"
		return false
	}
	view := &model.tasks
	if !model.taskScopeMatches(event) {
		return false
	}
	if previous := view.snapshot; previous != nil {
		seq, previousSeq := taskResultSequence(payload), taskResultSequence(*previous)
		if seq < previousSeq {
			return false
		}
		if seq == previousSeq {
			if !payload.Equal(*previous) {
				model.errorSummary = "stream_protocol_error: conflicting task progress snapshot"
				return false
			}
			view.hydration, view.errorText = taskHydrationReady, ""
			return true
		}
	}
	anchor := model.taskPanelAnchor()
	owned := payload.Clone()
	view.snapshot = &owned
	view.sessionID, view.threadID = event.SessionID, event.ThreadID
	view.enabled, view.hydration, view.errorText = true, taskHydrationReady, ""
	view.expanded = payload.Counts.Total > 0 && payload.Counts.Completed != payload.Counts.Total
	if view.open {
		model.restoreTaskPanelAnchor(anchor)
	}
	return true
}

func (model Model) projectTaskSnapshot(requestID protocol.RequestID, event protocol.Event) (tea.Model, tea.Cmd) {
	query := model.tasks.query
	if query == nil || query.requestID != requestID || query.generation != model.tasks.generation || !query.accepted {
		return model, nil
	}
	if !model.taskScopeMatches(event) {
		return model, nil
	}
	payload, err := protocol.DecodeTaskProgressSnapshot(event)
	if err != nil {
		model.failTaskQuery(query.generation, "stream_protocol_error: invalid task progress snapshot")
		return model, nil
	}
	model.observeTaskProgress(event, payload)
	model.tasks.query = nil
	return model, nil
}

func (model Model) projectTaskUpdate(event protocol.Event) (tea.Model, tea.Cmd) {
	if !model.taskScopeMatches(event) {
		return model, nil
	}
	payload, err := protocol.DecodeTaskProgressUpdated(event)
	if err != nil {
		model.errorSummary = "stream_protocol_error: invalid task progress update"
		return model, nil
	}
	model.observeTaskProgress(event, payload)
	return model, nil
}

func (model Model) closeTaskPanel() Model {
	if model.tasks.snapshot == nil && model.tasks.query != nil {
		model.failTaskQuery(model.tasks.query.generation, "task_query_unavailable: task progress query was cancelled")
	}
	model.tasks.open = false
	model.tasks.generation++
	model.tasks.query = nil
	model.restoreComposer(model.tasks.draft)
	anchors := model.reading.anchors
	model.reading = model.tasks.reading
	model.reading.restore(anchors)
	return model
}

func (model Model) handleTasksKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste {
		return model, nil
	}
	switch key.Type {
	case tea.KeyEsc:
		return model.closeTaskPanel(), nil
	case tea.KeyCtrlC:
		model = model.closeTaskPanel()
		return model.handleKey(key)
	case tea.KeyUp:
		model.tasks.scroll = max(0, model.tasks.scroll-1)
	case tea.KeyDown:
		model.tasks.scroll = min(model.tasks.scroll+1, model.taskPanelScrollLimit())
	case tea.KeyPgUp:
		model.tasks.scroll = max(0, model.tasks.scroll-max(1, model.heightOrDefault()-3))
	case tea.KeyPgDown:
		model.tasks.scroll = min(model.tasks.scroll+max(1, model.heightOrDefault()-3), model.taskPanelScrollLimit())
	case tea.KeyHome:
		model.tasks.scroll = 0
	case tea.KeyEnd:
		model.tasks.scroll = model.taskPanelScrollLimit()
	case tea.KeyRunes:
		if string(key.Runes) == "r" {
			return model.queryTasks(false)
		}
	}
	return model, nil
}

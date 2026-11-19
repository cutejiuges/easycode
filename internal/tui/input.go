package tui

import (
	"fmt"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// inputPreview 只保存本地可编辑副本，不能驱动运行时执行。
type inputPreview struct {
	id     domain.InputID
	text   string
	state  protocol.InputState
	reason protocol.InputReason
}

type inputRetraction struct {
	draft composer
}

type retractFailedMsg struct{ err error }

func (model *Model) refreshState() {
	model.state = stateIdle
	if model.activeTurnID.Valid() || model.activeCompactionID.Valid() {
		model.state = stateStreaming
	}
	if model.pending != nil {
		model.state = stateSubmitting
	}
}

func (model Model) canRetainInput(text string) bool {
	total := len(text)
	for _, input := range model.inputs {
		total += len(input.text)
	}
	return len(model.inputs) < domain.MaxPendingInputs && total <= 16<<20
}

func (model Model) projectControlItem(item protocol.ControlItem) (tea.Model, tea.Cmd) {
	if err := item.Validate(); err != nil {
		model.errorSummary = "stream_protocol_error: invalid control item"
		return model, nil
	}
	if event, ok := item.ContextEvent(); ok {
		if protocol.IsCompactionPresentation(event) {
			return model.projectCompactionPresentation(event)
		}
		if event.Kind == protocol.EventTaskProgressSnapshot {
			return model.projectTaskSnapshot(item.RequestID(), event)
		}
		if event.Kind == protocol.EventEffortSnapshot {
			return model.projectEffortEvent(item.RequestID(), event)
		}
		if event.Kind == protocol.EventCacheSnapshot || event.Kind == protocol.EventContextSnapshot {
			if item.RequestID() != model.queryRequestID || model.queryRequestID == "" {
				return model, nil
			}
			if event.Kind == protocol.EventCacheSnapshot {
				if model.queryKind != protocol.CommandQueryCache {
					return model, nil
				}
				return model.projectCacheEvent(event)
			}
			if model.queryOperationGeneration != model.effortOperationGeneration {
				if payload, err := protocol.DecodeContextSnapshot(event); err == nil {
					model.observeEffort(payload.Effort, false)
				}
				return model, nil
			}
			if model.queryKind != protocol.CommandQueryContext {
				return model, nil
			}
		}
		return model.projectContextEvent(event)
	}
	if event, ok := item.TurnEvent(); ok {
		return model.Update(RuntimeEventMsg{Event: event})
	}
	if result, ok := item.CommandResult(); ok {
		switch result.CommandKind() {
		case protocol.CommandQueryTasks:
			return model.projectTaskQueryResult(result)
		case protocol.CommandQueryEffort, protocol.CommandSetEffort:
			return model.projectEffortResult(result)
		case protocol.CommandRenameSession:
			return model.projectSessionTitleResult(result)
		case protocol.CommandQueryCache, protocol.CommandQueryContext, protocol.CommandCompactContext, protocol.CommandCancelCompaction:
			return model.projectContextResult(result)
		case protocol.CommandSubmitConversationalInput:
			if model.pending == nil {
				model.errorSummary = "stream_protocol_error: unexpected admission"
				return model, nil
			}
			if result.Status() == protocol.CommandStatusAccepted {
				text := model.pending.text
				model.inputs = append(model.inputs, inputPreview{id: result.EntryID(), text: text})
				model.composer.history.add(text)
				model.composer.clear()
			} else {
				model.interruptNext = false
				model.restoreComposer(*model.pending)
				summary, _ := result.Error()
				model.errorSummary = string(summary.Code()) + ": " + summary.Message()
			}
			model.pending = nil
			model.pendingCancel = nil
			model.refreshState()
		case protocol.CommandRetractInputs:
			if model.retracting == nil {
				return model, nil
			}
			if result.Status() == protocol.CommandStatusAccepted {
				model.restoreComposer(model.retracting.draft)
			} else {
				summary, _ := result.Error()
				model.errorSummary = string(summary.Code()) + ": " + summary.Message()
			}
			model.retracting = nil
		}
		return model, nil
	}
	if status, ok := item.InputStatus(); ok {
		for index := range model.inputs {
			if model.inputs[index].id != status.EntryID() {
				continue
			}
			switch status.State() {
			case protocol.InputCommitted:
				model.inputs = append(model.inputs[:index:index], model.inputs[index+1:]...)
			case protocol.InputReturned:
				if status.Reason() == protocol.InputReasonRetracted {
					model.inputs = append(model.inputs[:index:index], model.inputs[index+1:]...)
				} else {
					model.inputs[index].state, model.inputs[index].reason = status.State(), status.Reason()
				}
			default:
				model.inputs[index].state, model.inputs[index].reason = status.State(), status.Reason()
			}
			break
		}
	}
	return model, nil
}

func (model Model) hasQueuedInput() bool {
	for _, input := range model.inputs {
		if input.state == "" {
			return true
		}
	}
	return false
}

func (model Model) retractQueuedInputs() (tea.Model, tea.Cmd) {
	var ids []domain.InputID
	var texts []string
	for _, input := range model.inputs {
		if input.state == "" {
			ids = append(ids, input.id)
			texts = append(texts, input.text)
		}
	}
	if len(ids) == 0 {
		return model, nil
	}
	if model.composer.text != "" {
		texts = append(texts, model.composer.text)
	}
	draft := model.composer
	if err := draft.replace(0, len(draft.text), strings.Join(texts, "\n")); err != nil {
		model.errorSummary = "input_too_large: queued inputs do not fit the draft; stop and take returned inputs individually"
		return model, nil
	}
	model.retracting = &inputRetraction{draft: draft}
	target, ctx := model.session, model.submitContext
	return model, func() tea.Msg {
		_, err := target.Retract(ctx, ids)
		if err != nil {
			return retractFailedMsg{err: err}
		}
		return nil
	}
}

func (model Model) handleReturnedInput(take bool) (tea.Model, tea.Cmd) {
	for index, input := range model.inputs {
		if input.state != protocol.InputReturned {
			continue
		}
		if take {
			text := input.text
			if model.composer.text != "" {
				text += "\n" + model.composer.text
			}
			draft := model.composer
			if err := draft.replace(0, len(draft.text), text); err != nil {
				model.errorSummary = "input_too_large: returned input does not fit the draft"
				return model, nil
			}
			model.composer = draft
		}
		model.inputs = append(model.inputs[:index:index], model.inputs[index+1:]...)
		break
	}
	return model, nil
}

func (model Model) renderInputs() string {
	if len(model.inputs) == 0 {
		return ""
	}
	budget := max(0, model.heightOrDefault()-model.composerChatRows()-model.nonEffortStatusRows()-4)
	if budget == 0 {
		return ""
	}
	hint := budget >= 2
	visible := budget
	if hint {
		visible--
	}
	summary := len(model.inputs) > visible && budget >= 3
	if summary {
		visible--
	}
	visible = min(len(model.inputs), visible)
	var view strings.Builder
	line := func(text string) {
		view.WriteString(ansi.Truncate(text, max(1, model.widthOrDefault()), "…"))
		view.WriteByte('\n')
	}
	for _, input := range model.inputs[:visible] {
		state := string(input.state)
		if state == "" {
			state = "queued"
		}
		preview := truncateRunes(strings.ReplaceAll(safeComposerText(input.text), "\n", " ↵ "), max(1, model.widthOrDefault()-12))
		line(fmt.Sprintf("[%s] %s", state, strings.TrimRight(preview, " ")))
	}
	if summary {
		line(fmt.Sprintf("%d more queued or returned inputs", len(model.inputs)-visible))
	}
	if hint {
		text := "Up/Esc: edit queued · Ctrl+R: take returned · Ctrl+D: discard returned"
		if model.widthOrDefault() < 64 {
			text = "Esc edit · ^R take · ^D drop"
		}
		line(text)
	}
	return view.String()
}

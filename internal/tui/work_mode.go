package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

const planCommandName = "plan"

type workModeSession interface {
	SetWorkMode(context.Context, protocol.RequestID, domain.WorkMode) (protocol.CommandResult, error)
}

type workModeView struct {
	mode     domain.WorkMode
	pending  *workModeRequest
	sequence uint64
	enabled  bool
}

type workModeRequest struct {
	id           protocol.RequestID
	cancel       context.CancelFunc
	consumeDraft bool
}

type workModeDeliveryFailedMsg struct {
	id  protocol.RequestID
	err error
}

func (model Model) submitPlanCommand(remainder string) (tea.Model, tea.Cmd) {
	target := domain.WorkModePlan
	argument := strings.TrimSpace(remainder)
	if strings.Contains(model.composer.text, "\n") || (argument != "" && argument != "off") {
		if !strings.Contains(model.composer.text, "\n") && argument == "review" {
			return model.openPlanReviewCommand(), nil
		}
		model.errorSummary = "invalid_command: use /plan, /plan off or /plan review on one line"
		return model, nil
	}
	if argument == "off" {
		target = domain.WorkModeDefault
	}
	return model.requestWorkMode(target, true)
}

func (model Model) switchWorkMode() (tea.Model, tea.Cmd) {
	target := domain.WorkModePlan
	if model.workMode.mode == domain.WorkModePlan {
		target = domain.WorkModeDefault
	}
	return model.requestWorkMode(target, false)
}

func (model Model) requestWorkMode(mode domain.WorkMode, consumeDraft bool) (tea.Model, tea.Cmd) {
	if model.activeTurnID.Valid() || model.activeCompactionID.Valid() || model.pendingContext != "" || model.hasQueuedInput() {
		model.errorSummary = "session_busy: finish the current work or handle pending inputs, then retry"
		return model, nil
	}
	target, ok := model.session.(workModeSession)
	if !ok || model.outputs == nil {
		model.errorSummary = "invalid_command: work mode control is unavailable"
		return model, nil
	}
	model.workMode.sequence++
	id := protocol.RequestID(fmt.Sprintf("tui-work-mode-%d", model.workMode.sequence))
	ctx, cancel := context.WithCancel(model.submitContext)
	model.workMode.pending = &workModeRequest{id: id, cancel: cancel, consumeDraft: consumeDraft}
	model.clearEffortFeedback()
	model.errorSummary = ""
	return model, func() tea.Msg {
		if _, err := target.SetWorkMode(ctx, id, mode); err != nil {
			return workModeDeliveryFailedMsg{id: id, err: err}
		}
		// 有序控制流承载唯一权威确认。
		return nil
	}
}

func (model *Model) finishWorkModeRequest(failure string) {
	model.workMode.pending.cancel()
	model.workMode.pending = nil
	model.errorSummary = failure
}

func (model Model) projectWorkModeResult(result protocol.CommandResult) (tea.Model, tea.Cmd) {
	if request := model.workMode.pending; request != nil && request.id == result.RequestID() {
		if summary, rejected := result.Error(); rejected {
			model.finishWorkModeRequest(string(summary.Code()) + ": " + summary.Message())
		}
	}
	return model, nil
}

func (model Model) projectWorkModeSnapshot(id protocol.RequestID, event protocol.Event) (tea.Model, tea.Cmd) {
	request := model.workMode.pending
	if request == nil || request.id != id || !model.matchesChatScope(event) {
		return model, nil
	}
	snapshot, err := protocol.DecodeWorkModeSnapshot(event)
	if err != nil {
		model.finishWorkModeRequest("stream_protocol_error: invalid work mode snapshot")
		return model, nil
	}
	model.workMode.mode = snapshot.Mode
	if snapshot.Mode == domain.WorkModeDefault {
		model.clearPlanReviewOffer()
	}
	if request.consumeDraft {
		model.composer.clear()
	}
	model.finishWorkModeRequest("")
	return model, nil
}

func (model Model) workModeLine(hint bool) string {
	if !model.workMode.enabled {
		return ""
	}
	width := model.widthOrDefault()
	label := "Default"
	if model.workMode.mode == domain.WorkModePlan {
		label = "Plan"
		if width >= ansi.StringWidth("Plan mode · read-only") {
			label = "Plan mode · read-only"
		}
	}
	if width < ansi.StringWidth(label) {
		return ""
	}
	if hint {
		instruction := " · Shift+Tab: switch mode"
		if model.workMode.pending != nil {
			instruction = " · Esc: cancel mode change"
		}
		if model.planReviewReady() && width >= ansi.StringWidth(label+" · /plan review") {
			instruction = " · /plan review"
		}
		if width >= ansi.StringWidth(label+instruction) {
			label += instruction
		}
	}
	return label + "\n"
}

func (model Model) composerWorkModeView(view string) string {
	if renderedChatRows(view) < model.heightOrDefault() {
		return view + model.workModeLine(true)
	}
	return view
}

func (model Model) modalWorkModeView(view string) string {
	if status := model.workModeLine(false); status != "" && renderedChatRows(view) < model.heightOrDefault() {
		return strings.TrimSuffix(view, "\n") + "\n" + status
	}
	return view
}

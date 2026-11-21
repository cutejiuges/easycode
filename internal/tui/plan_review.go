package tui

import (
	"context"
	"fmt"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type planReviewSession interface {
	ApprovePlan(context.Context, protocol.RequestID, protocol.PlanReviewRef) (protocol.CommandResult, error)
}

type planReviewView struct {
	offer            protocol.PlanReviewOffer
	terminal         domain.TurnID
	confirmation     *planReviewConfirmation
	starting         protocol.RequestID
	suspendAutomatic bool
	open             bool
	implement        bool
	viewport         planReviewViewport
}

type planReviewConfirmation struct {
	id     protocol.RequestID
	cancel context.CancelFunc
	input  domain.InputID
}

type planReviewDeliveryFailedMsg struct {
	id  protocol.RequestID
	err error
}

func (view planReviewView) waiting() bool { return view.confirmation != nil || view.starting != "" }

func (model Model) planReviewReady() bool {
	_, supported := model.session.(planReviewSession)
	return supported && model.outputs != nil && model.planReview.offer.Ref.Sample.TurnID.Valid() &&
		model.workMode.mode == domain.WorkModePlan && model.chatComposerEditable() &&
		!model.activeTurnID.Valid() && !model.activeCompactionID.Valid() && model.pendingContext == "" &&
		!model.planReview.waiting() && len(model.inputs) == 0
}

func (model Model) projectPlanReviewOffer(offer protocol.PlanReviewOffer) Model {
	if model.workMode.mode != domain.WorkModePlan || offer.Ref.SessionID != model.tasks.sessionID ||
		offer.Ref.ThreadID != model.tasks.threadID || offer.Ref.Sample.TurnID != model.planReview.terminal ||
		offer.Ref.Sample.TurnID == model.planReview.offer.Ref.Sample.TurnID || model.planReview.waiting() {
		return model
	}
	model.planReview.offer = offer
	if model.planReviewReady() && model.composer.text == "" && model.candidateRows() == 0 &&
		!model.reading.detached && !model.planReview.suspendAutomatic {
		model.openPlanReview()
	}
	return model
}

func (model Model) openPlanReviewCommand() Model {
	if !model.planReviewReady() {
		model.errorSummary = "plan_review_unavailable: there is no current plan ready for review"
		return model
	}
	model.composer.clear()
	model.openPlanReview()
	return model
}

func (model *Model) openPlanReview() {
	model.planReview.open = true
	model.planReview.implement = false
	model.planReview.viewport = newPlanReviewViewport(model.planReview.offer.Markdown)
	model.errorSummary = ""
	model.clearEffortFeedback()
}

func (model *Model) closePlanReview() {
	model.planReview.open = false
	model.planReview.implement = false
	model.planReview.viewport = planReviewViewport{}
}

func (model *Model) clearPlanReviewOffer() {
	model.closePlanReview()
	model.planReview.offer = protocol.PlanReviewOffer{}
	model.planReview.terminal = ""
}

func (model *Model) closePlanReviewControl() {
	if request := model.planReview.confirmation; request != nil {
		if request.input.Valid() {
			// 已接纳的确认本身足以证明模式已切换，即使流在快照前关闭。
			model.workMode.mode = domain.WorkModeDefault
		}
		model.finishPlanReviewConfirmation(model.errorSummary)
	}
	model.planReview.starting = ""
	model.clearPlanReviewOffer()
}

func (model Model) handlePlanReviewKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste || key.Alt {
		return model, nil
	}
	if request := model.planReview.confirmation; request != nil {
		if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
			request.cancel()
		}
		return model, nil
	}
	if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
		model.closePlanReview()
		return model, nil
	}
	if !model.planReviewFits() {
		return model, nil
	}
	viewport := &model.planReview.viewport
	switch key.Type {
	case tea.KeyUp, tea.KeyDown, tea.KeyTab:
		model.planReview.implement = !model.planReview.implement
	case tea.KeyPgUp:
		viewport.first = max(0, viewport.first-model.planReviewBodyRows())
	case tea.KeyPgDown:
		viewport.first += model.planReviewBodyRows()
	case tea.KeyHome:
		viewport.first = 0
	case tea.KeyEnd:
		viewport.first = int(^uint(0) >> 1)
	case tea.KeyRunes:
		if string(key.Runes) == "r" && !viewport.forceRaw {
			viewport.raw = !viewport.raw
			viewport.first = 0
		}
	case tea.KeyEnter:
		if model.planReview.implement {
			return model.submitPlanReview()
		}
		model.closePlanReview()
	}
	return model, nil
}

func (model Model) submitPlanReview() (tea.Model, tea.Cmd) {
	target, ok := model.session.(planReviewSession)
	if !ok || !model.canRetainInput(domain.PlanImplementationInput(model.planReview.offer.Markdown)) {
		model.errorSummary = "plan_review_unavailable: the implementation input cannot be retained"
		return model, nil
	}
	model.workMode.sequence++
	id := protocol.RequestID(fmt.Sprintf("tui-plan-approval-%d", model.workMode.sequence))
	ctx, cancel := context.WithCancel(model.submitContext)
	model.planReview.confirmation = &planReviewConfirmation{id: id, cancel: cancel}
	model.errorSummary = ""
	ref := model.planReview.offer.Ref
	return model, func() tea.Msg {
		if _, err := target.ApprovePlan(ctx, id, ref); err != nil {
			return planReviewDeliveryFailedMsg{id: id, err: err}
		}
		return nil
	}
}

func (model *Model) finishPlanReviewConfirmation(failure string) {
	model.planReview.confirmation.cancel()
	model.planReview.confirmation = nil
	model.planReview.implement = false
	model.errorSummary = failure
}

func (model Model) projectPlanReviewResult(result protocol.CommandResult) Model {
	request := model.planReview.confirmation
	if request == nil || request.id != result.RequestID() || request.input.Valid() {
		return model
	}
	if failure, rejected := result.Error(); rejected {
		model.finishPlanReviewConfirmation(string(failure.Code()) + ": " + failure.Message())
		return model
	}
	text := domain.PlanImplementationInput(model.planReview.offer.Markdown)
	model.inputs = append(model.inputs, inputPreview{id: result.EntryID(), text: text, implementation: true})
	accepted := *request
	accepted.input = result.EntryID()
	model.planReview.confirmation = &accepted
	return model
}

func (model Model) projectPlanReviewMode(event protocol.Event) Model {
	request := model.planReview.confirmation
	ref := model.planReview.offer.Ref
	if event.SessionID != ref.SessionID || event.ThreadID != ref.ThreadID || !request.input.Valid() {
		return model
	}
	snapshot, err := protocol.DecodeWorkModeSnapshot(event)
	if err != nil || snapshot.Mode != domain.WorkModeDefault {
		model.errorSummary = "stream_protocol_error: invalid plan implementation mode"
		return model
	}
	model.workMode.mode = snapshot.Mode
	model.planReview.starting = request.id
	model.finishPlanReviewConfirmation("")
	model.clearPlanReviewOffer()
	return model
}

func (model *Model) rememberCommittedPlanInput(payload protocol.UserInputCommittedPayload) {
	for _, input := range model.inputs {
		if input.implementation && input.id == payload.InputID {
			model.composer.history.add(payload.Text)
			return
		}
	}
}

package runtime

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// ApprovePlan 由当前交互宿主确认指定方案，正文由控制 owner 保持。
func (session *ChatSession) ApprovePlan(ctx context.Context, requestID protocol.RequestID, ref protocol.PlanReviewRef) (protocol.CommandResult, error) {
	command, err := protocol.NewApprovePlanCommand(requestID, ref)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return session.submitWorkModeControl(ctx, command)
}

func (state *agentLoopRunState) offerPlanReview(turn *activeLoopTurn) {
	runtime := state.loop.runtime
	projector := runtime.providerOperations.planProposal
	if projector == nil || state.loop.interactiveStopped == nil ||
		resumeSignalClosed(state.loop.interactiveStopped) || resumeSignalClosed(state.runContextDone) ||
		!turn.completed || turn.worker.cancelRequested || turn.lastSample.TurnID != turn.turnID ||
		turn.lastSample.Validate() != nil || turn.profile.WorkMode() != domain.WorkModePlan ||
		state.selectedProfile.WorkMode() != domain.WorkModePlan || state.mode != loopModeAccepting ||
		!state.idleForWorkMode() || runtime.poisoned.Load() || runtime.journal.Poisoned() {
		return
	}
	markdown, ok := projector.ProjectPlanProposal(turn.lastSample)
	if !ok {
		return
	}
	ref := protocol.PlanReviewRef{SessionID: runtime.sessionID, ThreadID: runtime.threadID, Sample: turn.lastSample}
	item, err := protocol.NewPlanReviewOfferItem(turn.input.requestID, ref, markdown)
	if err != nil {
		return
	}
	state.planReview = protocol.PlanReviewOffer{Ref: ref, Markdown: markdown}
	state.loop.outputs <- item
}

func (state *agentLoopRunState) approvePlan(request loopCommandRequest) {
	if !state.canChangeWorkMode(request) {
		return
	}
	offer := state.planReview
	if state.selectedProfile.WorkMode() != domain.WorkModePlan || request.command.PlanReviewRef() != offer.Ref {
		state.reject(request, protocol.ControlErrorPlanUnavailable, true)
		return
	}
	text := domain.PlanImplementationInput(offer.Markdown)
	if len(text) > state.loop.config.MaxQueuedBytes {
		state.reject(request, protocol.ControlErrorInputQueueFull, true)
		return
	}
	id, err := domain.GenerateInputID()
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidInput, true)
		return
	}
	input, err := domain.NewUserInput(id, text)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidInput, true)
		return
	}
	association, _ := inputAssociation(domain.TurnID(id), 0, []domain.UserInput{input})
	if !state.loop.runtime.inputEncodingFits(association, []domain.UserInput{input}) {
		state.reject(request, protocol.ControlErrorInputEncodingLimit, true)
		return
	}
	state.startApprovedPlan(request, input)
}

func (state *agentLoopRunState) startApprovedPlan(request loopCommandRequest, input domain.UserInput) {
	requestID := request.command.RequestID()
	profile, err := state.selectedProfile.WithWorkMode(domain.WorkModeDefault)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	mode, err := state.workModeConfirmation(requestID, domain.WorkModeDefault)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	result, err := protocol.NewAcceptedInputResult(request.command, protocol.CommandDispositionStarting, input.ID())
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	if !state.acceptWorkModeControl(request) {
		return
	}
	state.selectedProfile = profile
	state.enqueueInput(queuedInput{value: input, automatic: true, requestID: requestID})
	state.emitResult(request, result)
	state.loop.outputs <- mode
	// 启动和普通输入使用同一会话 context，确认请求取消不影响已接纳的实施。
	state.startNext()
}

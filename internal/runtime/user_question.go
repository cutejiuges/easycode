package runtime

import (
	"context"
	"fmt"
	"sync"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type questionState struct {
	mu     sync.Mutex
	active *pendingQuestion
}
type pendingQuestion struct {
	request    protocol.UserQuestionRequestedPayload
	turnID     domain.TurnID
	invocation tool.QuestionInvocation
	preflight  provider.ToolOutputPreflight
	parentDone <-chan struct{}
	done       chan struct{}
	answers    tool.QuestionAnswerSet
	received   bool
	closed     bool
}

type questionResponder struct {
	runtime   *Runtime
	turnID    domain.TurnID
	emit      Emitter
	allowance int
	preflight provider.ToolOutputPreflight
	pending   *pendingQuestion
}

func (r *questionResponder) Answer(ctx context.Context, v tool.QuestionInvocation) (tool.QuestionAnswerSet, error) {
	if err := ctx.Err(); err != nil {
		return tool.QuestionAnswerSet{}, err
	}
	id, err := r.runtime.newQuestionRequestID()
	if err != nil {
		return tool.QuestionAnswerSet{}, fmt.Errorf("generate question identity failed")
	}
	request := protocol.UserQuestionRequestedPayload{RequestID: id, InvocationID: string(v.InvocationID()), Fingerprint: v.Input().Fingerprint(), Input: v.Input(), ResultAllowance: r.allowance}
	event, err := protocol.NewUserQuestionRequested(request)
	if err != nil {
		return tool.QuestionAnswerSet{}, err
	}
	p := &pendingQuestion{request: request, turnID: r.turnID, invocation: v, preflight: r.preflight, parentDone: ctx.Done(), done: make(chan struct{})}
	r.runtime.questions.mu.Lock()
	if r.runtime.questions.active != nil {
		r.runtime.questions.mu.Unlock()
		return tool.QuestionAnswerSet{}, fmt.Errorf("another user question is active")
	}
	r.runtime.questions.active = p
	r.pending = p
	r.runtime.questions.mu.Unlock()
	r.emit(r.runtime.decorate(event, r.turnID))
	select {
	case <-p.done:
	case <-ctx.Done():
	}
	r.runtime.questions.mu.Lock()
	answers, received := p.answers, p.received
	r.runtime.questions.mu.Unlock()
	// 已接收答案的义务不随随后发生的取消撤回。
	if received {
		return answers, nil
	}
	if err := ctx.Err(); err != nil {
		return tool.QuestionAnswerSet{}, err
	}
	return tool.QuestionAnswerSet{}, fmt.Errorf("question closed without an answer")
}

func (r *Runtime) acceptQuestionAnswer(turnID domain.TurnID, response protocol.UserQuestionResponse) protocol.ControlErrorCode {
	if response.Validate() != nil {
		return protocol.ControlErrorQuestionAnswerInvalid
	}
	r.questions.mu.Lock()
	p := r.questions.active
	if p == nil || p.closed {
		r.questions.mu.Unlock()
		return protocol.ControlErrorNoActiveQuestion
	}
	if p.turnID != turnID {
		r.questions.mu.Unlock()
		return protocol.ControlErrorTurnMismatch
	}
	if p.received || p.request.RequestID != response.RequestID || p.request.InvocationID != response.InvocationID || p.request.Fingerprint != response.Fingerprint {
		r.questions.mu.Unlock()
		return protocol.ControlErrorQuestionMismatch
	}
	r.questions.mu.Unlock()
	result, err := tool.NewQuestionInvocationResult(p.invocation, response.Answers)
	if err != nil {
		return protocol.ControlErrorQuestionAnswerInvalid
	}
	result, err = tool.FinalizeResult(result, p.request.ResultAllowance, tool.BudgetPreserved)
	output, outputErr := tool.NewModelOutput(result)
	if err != nil || outputErr != nil || session.PreflightQuestionResult(session.Identity{SessionID: r.sessionID, ThreadID: r.threadID}, turnID, result) != nil || p.preflight.Validate(output) != nil {
		return protocol.ControlErrorQuestionAnswerInvalid
	}
	r.questions.mu.Lock()
	defer r.questions.mu.Unlock()
	if r.questions.active != p || p.closed || p.received {
		return protocol.ControlErrorQuestionMismatch
	}
	select {
	case <-p.parentDone:
		return protocol.ControlErrorNoActiveQuestion
	default:
	}
	p.answers = response.Answers
	p.received = true
	close(p.done)
	return ""
}

func (r *Runtime) closeQuestion(p *pendingQuestion) {
	if p == nil {
		return
	}
	r.questions.mu.Lock()
	if r.questions.active == p {
		p.closed = true
		r.questions.active = nil
		if !p.received {
			close(p.done)
		}
	}
	r.questions.mu.Unlock()
}

func (r *Runtime) executeExclusiveCall(ctx context.Context, emit Emitter, turnID domain.TurnID, inv tool.Invocation) (tool.InvocationResult, bool, error) {
	if inv.Capability() != tool.CapabilityQuestion {
		return r.executeApproved(ctx, emit, turnID, inv)
	}
	finishError := func(status tool.ResultStatus, code, message string) (tool.InvocationResult, bool, error) {
		result := tool.NewInvocationErrorResult(inv, status, code, message)
		err := r.persistToolResult(context.WithoutCancel(ctx), turnID, &result)
		return result, false, err
	}
	if r.resultBudget.BodyExhausted() {
		return finishError(tool.ResultError, "result_budget_exceeded", "Question not asked: result budget exhausted")
	}
	if !r.interactiveQuestions {
		return finishError(tool.ResultDenied, "user_input_unavailable", "Interactive user input is unavailable")
	}
	v, _ := inv.Question()
	call, _ := tool.NewQuestionReadyCall(v.ProviderCallID(), v.Input())
	if r.toolPolicy.Decide(call) != tool.PolicyAllow {
		return finishError(tool.ResultDenied, "permission_denied", "User questions are not allowed")
	}
	allowance, err := r.resultAllowance(tool.CapabilityQuestion)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	minimal := make([]tool.QuestionAnswer, len(v.Input().Questions()))
	for i, q := range v.Input().Questions() {
		minimal[i] = tool.QuestionAnswer{QuestionID: q.ID, Skipped: true}
	}
	answers, err := tool.NewQuestionAnswerSet(minimal)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	minimum, err := tool.NewQuestionInvocationResult(v, answers)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	if _, err = tool.FinalizeResult(minimum, allowance, tool.BudgetPreserved); err != nil {
		return finishError(tool.ResultError, "result_budget_exceeded", "Question not asked: result budget exhausted")
	}
	preparer, ok := r.conversation.(provider.ToolResultPreparer)
	if !ok {
		return tool.InvocationResult{}, false, fmt.Errorf("question output preparation is unavailable")
	}
	preflight, err := preparer.PrepareToolOutputPreflight(inv)
	if err != nil || preflight == nil {
		return tool.InvocationResult{}, false, fmt.Errorf("question output preflight is unavailable")
	}
	responder := &questionResponder{runtime: r, turnID: turnID, emit: emit, allowance: allowance, preflight: preflight}
	defer func() { r.closeQuestion(responder.pending) }()
	execution, err := r.executeExclusive(ctx, turnID, inv, exclusiveAction{
		execute: func(ctx context.Context) tool.InvocationResult { return r.questionExecutor.Execute(ctx, v, responder) },
		matches: func(result tool.InvocationResult) bool {
			return result.QuestionMetadata().Fingerprint == v.Input().Fingerprint()
		},
	})
	result := execution.Result
	if err == nil && responder.pending != nil {
		resolution := protocol.QuestionCancelled
		if result.Status() == tool.ResultSuccess {
			resolution = protocol.QuestionAnswered
			if result.QuestionMetadata().SkippedCount == result.QuestionMetadata().QuestionCount {
				resolution = protocol.QuestionSkipped
			}
		}
		if result.Status() == tool.ResultSuccess || result.Status() == tool.ResultCancelled {
			request := responder.pending.request
			e, buildErr := protocol.NewUserQuestionResolved(protocol.UserQuestionResolvedPayload{RequestID: request.RequestID, InvocationID: request.InvocationID, Fingerprint: request.Fingerprint, Resolution: resolution})
			if buildErr == nil {
				emit(r.decorate(e, turnID))
			}
		}
	}
	return result, false, err
}

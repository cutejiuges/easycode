package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
	"errors"
	"time"
)

type postHookCall struct {
	plan                  domain.PostHookPlan
	declarations          []domain.HookDeclaration
	trusted               bool
	finished              []domain.HookFinished
	terminal              *domain.PostHookTerminal
	history               session.ReplayedPostHooks
	detailSequence        uint64
	invocationFingerprint string
	resultFingerprint     string
}

func (r *Runtime) preparePostHooks(ctx context.Context, emit Emitter, turn domain.TurnID, inv tool.Invocation) (*postHookCall, domain.HookCode, error) {
	r.hooks.mu.Lock()
	snapshot, runner := r.hooks.snapshot.Clone(), r.hooks.runner
	r.hooks.mu.Unlock()
	if runner == nil {
		return nil, domain.HookOK, nil
	}
	ds, err := snapshot.Match(domain.HookPostToolUse, domain.HookCapability(inv.Capability()))
	if err != nil {
		code := domain.HookConfigInvalid
		if hookCode, ok := err.(domain.HookCode); ok {
			code = hookCode
		}
		return nil, code, err
	}
	if len(ds) == 0 {
		return nil, domain.HookOK, nil
	}
	if r.interactiveApprovals {
		if code := runner.Preflight(ctx); code != domain.HookOK {
			return nil, code, nil
		}
	}
	refs := make([]domain.HookReference, len(ds))
	model := false
	for i, d := range ds {
		refs[i] = d.Reference
		model = model || d.Reference.Feedback == domain.HookFeedbackModel
	}
	p, err := domain.NewHookPlan(string(inv.InvocationID()), domain.HookCapability(inv.Capability()), refs)
	if err != nil {
		return nil, domain.HookConfigInvalid, err
	}
	allowance, err := r.resultAllowance(inv.Capability())
	if err != nil {
		return nil, domain.HookConfigInvalid, err
	}
	if r.resultBudget == nil {
		return nil, domain.HookConfigInvalid, errors.New("post hooks require result budget owner")
	}
	ticket, err := r.resultBudget.Reserve(inv.Capability(), model)
	if err != nil {
		return nil, domain.HookConfigInvalid, err
	}
	r.resultTicket = ticket
	r.resultTicketInvocation = inv.InvocationID()
	r.resultTicketCapability = inv.Capability()
	r.resultTicketPost = true
	delivery := domain.HookDeliveryNone
	if model {
		delivery = domain.HookDeliveryUnavailable
		if tool.PostFeedbackAvailable(p, ticket.Reservation()) {
			delivery = domain.HookDeliveryAvailable
		}
	}
	plan, err := domain.NewPostHookPlan(p, allowance, ticket.Reservation(), delivery)
	if err != nil {
		return nil, domain.HookConfigInvalid, err
	}
	draft, err := session.NewToolPostHookPlannedDraft(turn, plan)
	if err != nil {
		return nil, domain.HookConfigInvalid, err
	}
	ready, err := session.NewToolCallReadyDraft(turn, inv, 0, 0)
	if err != nil {
		return nil, domain.HookConfigInvalid, err
	}
	state := &postHookCall{plan: plan, declarations: append([]domain.HookDeclaration(nil), ds...), invocationFingerprint: domain.HookDigest(ready.PayloadBytes())}
	if err = r.appendPostFacts(ctx, turn, state, protocol.PostHookPlanned, []session.RecordDraft{draft}); err != nil {
		if !r.poisoned.Load() && !r.journal.Poisoned() {
			if abortErr := ticket.AbortReservation(); abortErr != nil {
				return nil, domain.HookConfigInvalid, abortErr
			}
			r.resultTicket = nil
			r.resultTicketInvocation = ""
			r.resultTicketCapability = ""
			r.resultTicketPost = false
		}
		code := domain.HookConfigInvalid
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code = domain.HookCancelled
		}
		return nil, code, err
	}
	r.hooks.post[inv.InvocationID()] = state
	if !r.interactiveApprovals {
		return state, domain.HookTrustRequired, nil
	}
	for _, d := range ds {
		if ctx.Err() != nil {
			return state, domain.HookCancelled, nil
		}
		code, err := r.trustHook(ctx, emit, turn, inv, d)
		if err != nil {
			return state, domain.HookTrustDenied, err
		}
		if code != domain.HookOK {
			return state, code, nil
		}
	}
	state.trusted = true
	return state, domain.HookOK, nil
}

func (r *Runtime) postHookInput(turn domain.TurnID, inv tool.Invocation, result tool.InvocationResult) (domain.HookInput, error) {
	r.hooks.mu.Lock()
	workspace := r.hooks.workspace
	r.hooks.mu.Unlock()
	base, err := r.hookInput(turn, inv, workspace)
	if err != nil {
		return domain.HookInput{}, err
	}
	var response domain.HookToolResponse
	switch result.Capability() {
	case tool.CapabilityPatch:
		m := result.PatchMetadata()
		response, err = domain.NewHookPatchResponse(domain.HookPatchMetadata{FilePath: m.RelativePath(), ReplacementCount: m.ReplacementCount(), AddedLines: m.AddedLines(), RemovedLines: m.RemovedLines()})
	case tool.CapabilityWrite:
		m := result.WriteMetadata()
		response, err = domain.NewHookWriteResponse(domain.HookWriteMetadata{FilePath: m.RelativePath(), Operation: string(m.Operation()), Bytes: int64(m.Bytes()), Lines: m.Lines(), CreatedDirectories: m.CreatedDirectories()})
	case tool.CapabilityExec:
		m := result.ExecMetadata()
		exit, known := m.ExitCode()
		response, err = domain.NewHookExecResponse(domain.HookExecMetadata{Workdir: m.Workdir(), ExitKnown: known, ExitCode: exit, StdoutBytes: m.Stdout().ObservedBytes(), StderrBytes: m.Stderr().ObservedBytes(), OutputTruncated: m.OutputTruncated(), TimedOut: m.TimedOut(), Cancelled: m.Cancelled()})
	default:
		return domain.HookInput{}, domain.HookConfigInvalid
	}
	if err != nil {
		return domain.HookInput{}, err
	}
	return domain.NewPostHookInput(base.SessionID, base.ThreadID, base.TurnID, base.ToolUseID, base.ToolName, base.CWD, base.Input, response)
}

func postFeedback(state *postHookCall) []domain.FrozenHookFeedback {
	feedback := make([]domain.FrozenHookFeedback, 0, len(state.finished)+domain.MaxHookMatches)
	for _, f := range state.finished {
		feedback = append(feedback, f.Feedback)
	}
	if state.terminal != nil {
		feedback = append(feedback, state.terminal.SuffixFeedback...)
	}
	return feedback
}
func postSuffix(state *postHookCall, code domain.HookCode) ([]domain.FrozenHookFeedback, error) {
	prior := postFeedback(state)
	suffix := make([]domain.FrozenHookFeedback, 0)
	for i := len(state.finished); i < len(state.plan.Plan.Hooks); i++ {
		ref := state.plan.Plan.Hooks[i]
		if ref.Feedback != domain.HookFeedbackModel {
			continue
		}
		id, _ := domain.NewPostHookRunIdentity(state.plan, i)
		f, err := domain.NewFrozenHookFeedback(id, ref, domain.HookFeedbackCode, "", code, 1)
		if err != nil {
			return nil, err
		}
		before, err := tool.RenderPostFeedback(prior)
		if err != nil {
			return nil, err
		}
		prior = append(prior, f)
		after, err := tool.RenderPostFeedback(prior)
		if err != nil || len(after) > state.plan.Reservation {
			return nil, errors.New("post hook suffix reservation violated")
		}
		f.BudgetDelta = len(after) - len(before)
		prior[len(prior)-1] = f
		suffix = append(suffix, f)
	}
	return suffix, nil
}

func (r *Runtime) completePostHooks(ctx context.Context, turn domain.TurnID, result tool.InvocationResult, state *postHookCall, disposition domain.PostHookDisposition, code domain.HookCode, origin string, prefix []session.RecordDraft) error {
	suffix := []domain.FrozenHookFeedback{}
	if origin == "execution" && result.Status() == tool.ResultSuccess && state.trusted && state.plan.Delivery == domain.HookDeliveryAvailable {
		var err error
		suffix, err = postSuffix(state, code)
		if err != nil {
			return err
		}
	}
	feedback := append(postFeedback(state), suffix...)
	text, err := tool.RenderPostFeedback(feedback)
	if err != nil {
		return err
	}
	budget, err := domain.NewPostHookBudget(state.plan.Allowance, state.plan.Reservation, result.Budget().ActualBytes(), len(text))
	if err != nil {
		return err
	}
	terminal, err := domain.NewPostHookTerminal(state.plan, origin, disposition, code, len(state.finished), suffix, budget, domain.HookDigest([]byte(text)))
	if err != nil {
		return err
	}
	draft, err := session.NewToolPostHookCompletedDraft(turn, terminal)
	if err != nil {
		return err
	}
	if err = r.appendPostFacts(context.WithoutCancel(ctx), turn, state, protocol.PostHookCompleted, append(prefix, draft)); err != nil {
		return err
	}
	if r.resultTicket != nil {
		delta := 0
		if len(prefix) > 0 {
			delta += state.finished[len(state.finished)-1].Feedback.BudgetDelta
		}
		for _, f := range suffix {
			delta += f.BudgetDelta
		}
		if err = r.resultTicket.CommitFeedback(state.detailSequence, delta); err != nil {
			r.poisoned.Store(true)
			return err
		}
		if err = r.closeResultTicket(); err != nil {
			r.poisoned.Store(true)
			return err
		}
	}
	state.terminal = &terminal
	activityState := protocol.HookActivityCompleted
	switch disposition {
	case domain.PostHookSkipped:
		activityState = protocol.HookActivitySkipped
	case domain.PostHookCancelled:
		activityState = protocol.HookActivityState(domain.HookCancel)
	case domain.PostHookUncertain:
		activityState = protocol.HookActivityState(domain.HookUncertain)
	}
	ref := state.plan.Plan.Hooks[min(len(state.finished), len(state.plan.Plan.Hooks)-1)]
	if disposition == domain.PostHookCompleted && code != domain.HookOK {
		for _, f := range state.finished {
			if f.Result.Code == code {
				ref = f.Feedback.Reference
				break
			}
		}
	}
	r.emitHookActivity(turn, protocol.HookActivityPayload{InvocationID: string(result.InvocationID()), Reference: ref, State: activityState, Code: code})
	return nil
}

func (r *Runtime) runPostHooks(ctx context.Context, turn domain.TurnID, inv tool.Invocation, result tool.InvocationResult, state *postHookCall) error {
	if state == nil || state.terminal != nil {
		return nil
	}
	complete := func(disposition domain.PostHookDisposition, code domain.HookCode, prefix []session.RecordDraft) error {
		return r.completePostHooks(ctx, turn, result, state, disposition, code, "execution", prefix)
	}
	if result.Status() != tool.ResultSuccess {
		code := domain.HookToolNotSuccessful
		if result.Code() == string(domain.HookTrustDenied) {
			code = domain.HookTrustDenied
		}
		if result.Code() == string(domain.HookTrustRequired) {
			code = domain.HookTrustRequired
		}
		return complete(domain.PostHookSkipped, code, nil)
	}
	if !state.trusted {
		return errors.New("post hook trust is incomplete")
	}
	if ctx.Err() != nil {
		if err := complete(domain.PostHookCancelled, domain.HookCancelled, nil); err != nil {
			return err
		}
		return fault.Wrap(fault.CodeUserCancelled, "post hook cancelled", ctx.Err())
	}
	input, err := r.postHookInput(turn, inv, result)
	if ctx.Err() != nil {
		if err := complete(domain.PostHookCancelled, domain.HookCancelled, nil); err != nil {
			return err
		}
		return fault.Wrap(fault.CodeUserCancelled, "post hook cancelled", ctx.Err())
	}
	if err != nil {
		if errors.Is(err, domain.HookInputLimit) {
			return complete(domain.PostHookSkipped, domain.HookInputLimit, nil)
		}
		return err
	}
	r.hooks.mu.Lock()
	runner, now := r.hooks.runner, r.hooks.now
	r.hooks.mu.Unlock()
	remaining := r.hooks.remaining[inv.InvocationID()]
	if remaining == 0 {
		if _, exists := r.hooks.remaining[inv.InvocationID()]; !exists {
			remaining = 30 * time.Second
		}
	}
	for ordinal := len(state.finished); ordinal < len(state.declarations); ordinal++ {
		if ctx.Err() != nil {
			if err := complete(domain.PostHookCancelled, domain.HookCancelled, nil); err != nil {
				return err
			}
			return fault.Wrap(fault.CodeUserCancelled, "post hook cancelled", ctx.Err())
		}
		if remaining <= 0 {
			return complete(domain.PostHookSkipped, domain.HookExecutionBudgetExhausted, nil)
		}
		d := state.declarations[ordinal]
		id, _ := domain.NewPostHookRunIdentity(state.plan, ordinal)
		started, _ := session.NewHookExecutionStartedDraft(turn, id)
		if err = r.appendPostFacts(ctx, turn, state, protocol.PostHookStarted, []session.RecordDraft{started}); err != nil {
			if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && !r.journal.Poisoned() {
				if completeErr := complete(domain.PostHookCancelled, domain.HookCancelled, nil); completeErr != nil {
					return completeErr
				}
			}
			return err
		}
		r.emitHookActivity(turn, protocol.HookActivityPayload{InvocationID: string(inv.InvocationID()), Reference: d.Reference, State: "checking", Code: domain.HookOK})
		start := now()
		execution := runner.Run(ctx, id, d, input, min(time.Duration(d.Reference.TimeoutSeconds)*time.Second, remaining))
		remaining -= max(0, now().Sub(start))
		r.hooks.remaining[inv.InvocationID()] = remaining
		if _, validationErr := domain.NewHookExecution(execution.Result, d.Reference.Feedback, execution.Feedback); validationErr != nil || execution.Result.Identity != id || execution.Result.Origin != "execution" {
			execution = domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}}
		}
		frozen, err := tool.FreezePostFeedback(state.plan, postFeedback(state), execution.Result, execution.Feedback)
		if err != nil {
			return err
		}
		finished, err := session.NewHookExecutionFinishedDraft(turn, frozen)
		if err != nil {
			return err
		}
		state.finished = append(state.finished, frozen)
		resultStatus := frozen.Result.Status
		final := ordinal == len(state.declarations)-1 || resultStatus == domain.HookCancel || resultStatus == domain.HookUncertain
		if final {
			disposition, code := domain.PostHookCompleted, domain.HookOK
			for _, f := range state.finished {
				if code == domain.HookOK && f.Result.Code != domain.HookOK {
					code = f.Result.Code
				}
			}
			if resultStatus == domain.HookCancel {
				disposition, code = domain.PostHookCancelled, domain.HookCancelled
			}
			if resultStatus == domain.HookUncertain {
				disposition, code = domain.PostHookUncertain, domain.HookOutcomeUncertain
			}
			err = complete(disposition, code, []session.RecordDraft{finished})
		} else {
			err = r.appendPostFacts(context.WithoutCancel(ctx), turn, state, protocol.PostHookFinished, []session.RecordDraft{finished})
			if err == nil && r.resultTicket != nil {
				err = r.resultTicket.CommitFeedback(state.detailSequence, frozen.Feedback.BudgetDelta)
			}
		}
		if err != nil {
			r.poisoned.Store(true)
			return err
		}
		if !final {
			r.emitHookActivity(turn, protocol.HookActivityPayload{InvocationID: string(inv.InvocationID()), Reference: d.Reference, State: protocol.HookActivityState(resultStatus), Code: frozen.Result.Code, StdoutBytes: frozen.Result.StdoutBytes, StderrBytes: frozen.Result.StderrBytes})
		}
		if resultStatus == domain.HookUncertain {
			r.poisoned.Store(true)
			return fault.New(fault.CodeSessionWrite, "post hook cleanup is uncertain")
		}
		if resultStatus == domain.HookCancel {
			return fault.Wrap(fault.CodeUserCancelled, "post hook cancelled", context.Canceled)
		}
	}
	return nil
}

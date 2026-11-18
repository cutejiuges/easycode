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

func (r *Runtime) hookInput(turn domain.TurnID, inv tool.Invocation, workspace string) (domain.HookInput, error) {
	input, err := tool.HookInputForInvocation(inv)
	if err != nil {
		return domain.HookInput{}, err
	}
	name := ""
	view, err := r.toolCatalog.View(r.conversation.Family())
	if err != nil {
		return domain.HookInput{}, err
	}
	for _, f := range view.Facades() {
		if f.Capability() == inv.Capability() {
			name = f.Name()
			break
		}
	}
	profile, err := tool.HookProfileForInvocation(inv)
	if err != nil {
		return domain.HookInput{}, err
	}
	return domain.NewHookInput(r.sessionID, r.threadID, turn, string(inv.ProviderCallID()), name, workspace, input, profile)
}

func (r *Runtime) commitHookFacts(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	records, err := r.journal.AppendBatch(ctx, drafts)
	if err != nil {
		if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && !r.journal.Poisoned() {
			return nil, err
		}
		r.poisoned.Store(true)
		return nil, fault.Wrap(fault.CodeSessionWrite, "persist hook facts failed", err)
	}
	return records, nil
}
func (r *Runtime) emitHookActivity(turn domain.TurnID, p protocol.HookActivityPayload) {
	emitter := r.activityEmitter
	if p.Reference.Event == domain.HookStop {
		emitter = r.turnEmitter
	}
	if !r.interactiveApprovals || emitter == nil {
		return
	}
	e, err := protocol.NewHookActivity(p)
	if err == nil {
		emitter(r.decorate(e, turn))
	}
}

func (r *Runtime) writeHookGateResult(ctx context.Context, turn domain.TurnID, inv tool.Invocation, p domain.HookPlan, status domain.HookOutcome, code domain.HookCode, completed, blocking int, prefix []session.RecordDraft, recovery bool) (tool.InvocationResult, error) {
	state, err := r.executionCall(turn, inv)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	d, err := domain.NewHookGateDecision(p, status, code, completed, blocking)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	finished := append([]domain.HookFinished(nil), state.pre.Finished...)
	for _, draft := range prefix {
		if draft.EventKind() != session.EventHookExecutionFinished {
			return tool.InvocationResult{}, domain.HookConfigInvalid
		}
		f, decodeErr := domain.DecodeHookFinished(draft.PayloadBytes())
		if decodeErr != nil {
			return tool.InvocationResult{}, decodeErr
		}
		finished = append(finished, f.Clone())
	}
	selection, err := tool.NewRejectedInputSelection(state.origin, inv, p, finished, d)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	origin := tool.InputResultExecution
	if recovery {
		origin = tool.InputResultRecovery
	}
	evidence, err := tool.NewInputResultEvidence(selection, tool.ExecutionAdmission{}, origin)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	gate, err := session.NewToolHookGateDecidedDraft(turn, d)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	prefix = append(prefix, gate)
	toolStatus := tool.ResultError
	switch status {
	case domain.HookBlock:
		toolStatus = tool.ResultDenied
	case domain.HookCancel, domain.HookUncertain:
		toolStatus = tool.ResultCancelled
	}
	result := tool.NewInvocationErrorResult(inv, toolStatus, string(code), domain.HookSummary(p.Hooks[blocking], p.Capability, code))
	if recovery {
		result, err = tool.FreezeInputRewriteResult(result, evidence, tool.SafetyResultBytes, tool.BudgetRecovery)
	} else {
		result, err = r.freezeResultUsingEvidence(result, evidence)
	}
	if err != nil {
		return tool.InvocationResult{}, err
	}
	resultDraft, err := session.NewToolCallResultDraft(turn, result)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	prefix = append(prefix, resultDraft)
	if err = r.appendPreFacts(context.WithoutCancel(ctx), turn, state, prefix); err != nil {
		return tool.InvocationResult{}, err
	}
	if !recovery {
		if err = r.commitResultBudget(result); err != nil {
			r.poisoned.Store(true)
			return tool.InvocationResult{}, err
		}
	}
	if err = r.notifyToolResult(turn, result, state.resultSequence); err != nil {
		return tool.InvocationResult{}, err
	}
	return result, nil
}

func (r *Runtime) finishHookGate(ctx context.Context, turn domain.TurnID, inv tool.Invocation, p domain.HookPlan, status domain.HookOutcome, code domain.HookCode, completed, blocking int, prefix []session.RecordDraft) (tool.InvocationResult, bool, error) {
	result, err := r.writeHookGateResult(ctx, turn, inv, p, status, code, completed, blocking, prefix, false)
	if err != nil {
		return tool.InvocationResult{}, true, err
	}
	if status == domain.HookUncertain {
		r.poisoned.Store(true)
		return result, true, fault.New(fault.CodeSessionWrite, "hook execution outcome is uncertain")
	}
	if status == domain.HookCancel {
		return result, true, fault.Wrap(fault.CodeUserCancelled, "hook cancelled", context.Canceled)
	}
	return result, true, nil
}

func (r *Runtime) checkToolHooks(ctx context.Context, emit Emitter, turn domain.TurnID, inv tool.Invocation) (tool.InvocationResult, bool, error) {
	state, err := r.executionCall(turn, inv)
	if err != nil {
		return tool.InvocationResult{}, true, err
	}
	if r.poisoned.Load() || r.journal.Poisoned() {
		return tool.InvocationResult{}, true, fault.New(fault.CodeSessionWrite, "session journal is poisoned")
	}
	if state.hasResult {
		result, err := state.cachedResult()
		return result, true, err
	}
	if state.pre != nil {
		if state.pre.Decision == nil || state.pre.Decision.Status != domain.HookContinue {
			return tool.InvocationResult{}, true, fault.New(fault.CodeStreamProtocol, "hook gate is already in flight")
		}
		return tool.InvocationResult{}, false, nil
	}
	r.hooks.mu.Lock()
	snapshot, runner, workspace, now := r.hooks.snapshot.Clone(), r.hooks.runner, r.hooks.workspace, r.hooks.now
	r.hooks.mu.Unlock()
	if runner == nil {
		return tool.InvocationResult{}, false, nil
	}
	plainFailure := func(status tool.ResultStatus, code domain.HookCode) (tool.InvocationResult, bool, error) {
		result := tool.NewInvocationErrorResult(inv, status, string(code), "Hook stopped: "+string(code))
		err := r.persistToolResult(context.WithoutCancel(ctx), turn, &result)
		return result, true, err
	}
	if ctx.Err() != nil {
		return plainFailure(tool.ResultCancelled, domain.HookCancelled)
	}
	matched, err := snapshot.Match(domain.HookPreToolUse, domain.HookCapability(inv.Capability()))
	if err != nil {
		code, ok := err.(domain.HookCode)
		if !ok {
			code = domain.HookConfigInvalid
		}
		return plainFailure(tool.ResultError, code)
	}
	if len(matched) == 0 {
		return tool.InvocationResult{}, false, nil
	}
	input, err := r.hookInput(turn, inv, workspace)
	if err != nil {
		code := domain.HookInputLimit
		if errors.Is(err, domain.HookConfigInvalid) || errors.Is(err, domain.HookOutputInvalid) {
			code = domain.HookConfigInvalid
		}
		return plainFailure(tool.ResultError, code)
	}
	if r.interactiveApprovals {
		if code := runner.Preflight(ctx); code != domain.HookOK {
			status := tool.ResultError
			if code == domain.HookCancelled {
				status = tool.ResultCancelled
			}
			return plainFailure(status, code)
		}
	}
	refs := make([]domain.HookReference, len(matched))
	for i, d := range matched {
		refs[i] = d.Reference
	}
	p, err := domain.NewHookPlan(string(inv.InvocationID()), domain.HookCapability(inv.Capability()), refs)
	if err != nil {
		return tool.InvocationResult{}, true, err
	}
	planned, err := session.NewToolHookGatePlannedDraft(turn, p)
	if err != nil {
		return tool.InvocationResult{}, true, err
	}
	if err = r.appendPreFacts(ctx, turn, state, []session.RecordDraft{planned}); err != nil {
		if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && !r.poisoned.Load() {
			return plainFailure(tool.ResultCancelled, domain.HookCancelled)
		}
		return tool.InvocationResult{}, true, err
	}
	if !r.interactiveApprovals {
		return r.finishHookGate(ctx, turn, inv, p, domain.HookBlock, domain.HookTrustRequired, 0, 0, nil)
	}
	remaining := 30 * time.Second
	r.hooks.remaining[inv.InvocationID()] = remaining
	for ordinal, d := range matched {
		if ctx.Err() != nil {
			return r.finishHookGate(ctx, turn, inv, p, domain.HookCancel, domain.HookCancelled, ordinal, ordinal, nil)
		}
		code, trustErr := r.trustHook(ctx, emit, turn, p.Target(), p.Digest, d)
		if trustErr != nil {
			result, blocked, persistErr := r.finishHookGate(ctx, turn, inv, p, domain.HookBlock, domain.HookTrustDenied, ordinal, ordinal, nil)
			if persistErr != nil {
				return result, blocked, persistErr
			}
			return result, blocked, fault.New(fault.CodeTurnFailed, "hook trust request failed")
		}
		if code != domain.HookOK {
			status := domain.HookBlock
			if code == domain.HookCancelled {
				status = domain.HookCancel
			}
			return r.finishHookGate(ctx, turn, inv, p, status, code, ordinal, ordinal, nil)
		}
		if remaining <= 0 {
			return r.finishHookGate(ctx, turn, inv, p, domain.HookError, domain.HookTimedOut, ordinal, ordinal, nil)
		}
		id, _ := domain.NewHookRunIdentity(p, ordinal)
		started, _ := session.NewHookExecutionStartedDraft(turn, id)
		if err = r.appendPreFacts(ctx, turn, state, []session.RecordDraft{started}); err != nil {
			if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && !r.poisoned.Load() {
				return r.finishHookGate(ctx, turn, inv, p, domain.HookCancel, domain.HookCancelled, ordinal, ordinal, nil)
			}
			return tool.InvocationResult{}, true, err
		}
		start := now()
		execution := runner.Run(ctx, id, d, input, min(time.Duration(d.Reference.TimeoutSeconds)*time.Second, remaining))
		result := execution.Result
		remaining -= max(0, now().Sub(start))
		r.hooks.remaining[inv.InvocationID()] = remaining
		_, executionErr := domain.NewHookExecution(result, d.Reference.Feedback, execution.Feedback, execution.Replacement)
		if executionErr == nil {
			executionErr = execution.Replacement.ValidateAgainst(input.RewriteContext())
		}
		if executionErr == nil && !execution.Replacement.Empty() {
			_, executionErr = tool.RewriteInvocation(inv, execution.Replacement.Input())
		}
		if executionErr != nil || result.Identity != id || result.Origin != "execution" {
			result = domain.HookRunResult{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}
			execution.Replacement = domain.HookInputReplacement{}
		}
		effect, effectErr := domain.NewUnchangedHookInputEffect(input.Input)
		if effectErr == nil && !execution.Replacement.Empty() && !input.Input.Equal(execution.Replacement.Input()) {
			effect, effectErr = domain.NewReplacedHookInputEffect(input.Input, execution.Replacement.Input(), input.RewriteContext().Profile())
		}
		if effectErr != nil {
			r.poisoned.Store(true)
			return tool.InvocationResult{}, true, effectErr
		}
		frozen, err := domain.NewPreHookFinished(result, d.Reference, effect)
		if err != nil {
			r.poisoned.Store(true)
			return tool.InvocationResult{}, true, err
		}
		finished, err := session.NewHookExecutionFinishedDraft(turn, frozen)
		if err != nil {
			return tool.InvocationResult{}, true, err
		}
		if result.Status != domain.HookContinue {
			out, blocked, err := r.finishHookGate(ctx, turn, inv, p, result.Status, result.Code, ordinal+1, ordinal, []session.RecordDraft{finished})
			return out, blocked, err
		}
		batch := []session.RecordDraft{finished}
		if ordinal == len(matched)-1 {
			decision, _ := domain.NewHookGateDecision(p, domain.HookContinue, domain.HookOK, len(matched), -1)
			gate, _ := session.NewToolHookGateDecidedDraft(turn, decision)
			batch = append(batch, gate)
		}
		if err = r.appendPreFacts(context.WithoutCancel(ctx), turn, state, batch); err != nil {
			return tool.InvocationResult{}, true, err
		}
		if ordinal < len(matched)-1 {
			candidate, err := state.checkingInput()
			if err != nil {
				r.poisoned.Store(true)
				return tool.InvocationResult{}, true, err
			}
			current := inv
			original, _ := tool.HookInputForInvocation(inv)
			if !candidate.Equal(original) {
				current, err = tool.RewriteInvocation(inv, candidate)
			}
			if err == nil {
				input, err = r.hookInput(turn, current, workspace)
			}
			if err != nil {
				r.poisoned.Store(true)
				return tool.InvocationResult{}, true, err
			}
		}
	}
	return tool.InvocationResult{}, false, nil
}

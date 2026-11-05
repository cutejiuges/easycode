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
	var input domain.HookToolInput
	var err error
	switch inv.Capability() {
	case tool.CapabilityPatch:
		v, _ := inv.Patch()
		p := v.Input()
		rs := p.Replacements()
		replacements := make([]domain.HookReplacement, len(rs))
		for i, x := range rs {
			replacements[i] = domain.HookReplacement{OldText: x.OldText(), NewText: x.NewText(), ReplaceAll: x.ReplaceAll(), LineBased: x.LineBased()}
		}
		input, err = domain.NewHookPatchInput(p.FilePath(), replacements)
	case tool.CapabilityWrite:
		v, _ := inv.Write()
		input, err = domain.NewHookWriteInput(v.Input().FilePath(), v.Input().Content())
	case tool.CapabilityExec:
		v, _ := inv.Exec()
		p := v.Input()
		input, err = domain.NewHookExecInput(p.Command(), p.Workdir(), p.TimeoutMillis())
	default:
		return domain.HookInput{}, domain.HookConfigInvalid
	}
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
	return domain.NewHookInput(r.sessionID, r.threadID, turn, string(inv.ProviderCallID()), name, workspace, input)
}

func (r *Runtime) appendHookFacts(ctx context.Context, drafts []session.RecordDraft) error {
	if _, err := r.journal.AppendBatch(ctx, drafts); err != nil {
		if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && !r.journal.Poisoned() {
			return err
		}
		r.poisoned.Store(true)
		return fault.Wrap(fault.CodeSessionWrite, "persist hook facts failed", err)
	}
	return nil
}
func (r *Runtime) emitHookActivity(turn domain.TurnID, p protocol.HookActivityPayload) {
	if !r.interactiveApprovals || r.activityEmitter == nil {
		return
	}
	e, err := protocol.NewHookActivity(p)
	if err == nil {
		r.activityEmitter(r.decorate(e, turn))
	}
}

func (r *Runtime) writeHookGateResult(ctx context.Context, turn domain.TurnID, inv tool.Invocation, p domain.HookPlan, status domain.HookOutcome, code domain.HookCode, completed, blocking int, prefix []session.RecordDraft, recovery bool) (tool.InvocationResult, error) {
	d, err := domain.NewHookGateDecision(p, status, code, completed, blocking)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	draft, err := session.NewToolHookGateDecidedDraft(turn, d)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	prefix = append(prefix, draft)
	toolStatus := tool.ResultError
	switch status {
	case domain.HookBlock:
		toolStatus = tool.ResultDenied
	case domain.HookCancel, domain.HookUncertain:
		toolStatus = tool.ResultCancelled
	}
	result := tool.NewInvocationErrorResult(inv, toolStatus, string(code), domain.HookSummary(p.Hooks[blocking], p.Capability, code))
	if recovery {
		result, err = tool.FinalizeResult(result, tool.SafetyResultBytes, tool.BudgetRecovery)
	} else {
		result, err = r.freezeResult(result)
	}
	if err != nil {
		return tool.InvocationResult{}, err
	}
	resultDraft, err := session.NewToolCallResultDraft(turn, result)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	prefix = append(prefix, resultDraft)
	if err = r.appendHookFacts(context.WithoutCancel(ctx), prefix); err != nil {
		return tool.InvocationResult{}, err
	}
	if r.hookHistory != nil {
		for _, draft := range prefix {
			if draft.EventKind() == session.EventHookExecutionFinished {
				finished, decodeErr := domain.DecodeHookRunResult(draft.PayloadBytes())
				if decodeErr == nil {
					r.hookHistory.HookFinished(p.Hooks[finished.Identity.Ordinal], finished)
				}
			}
		}
	}
	r.notifyToolResult(turn, result)
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
	r.hooks.mu.Lock()
	snapshot, runner, workspace, now := r.hooks.snapshot.Clone(), r.hooks.runner, r.hooks.workspace, r.hooks.now
	cached, hasCached := r.hooks.completed[inv.InvocationID()]
	r.hooks.mu.Unlock()
	if hasCached {
		if cached.ProviderCallID() != inv.ProviderCallID() || cached.Capability() != inv.Capability() || cached.WireKind() != inv.WireKind() {
			return tool.InvocationResult{}, true, fault.New(fault.CodeStreamProtocol, "hook invocation replay mismatch")
		}
		return cached, true, nil
	}
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
	matched, err := snapshot.Match(domain.HookCapability(inv.Capability()))
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
		if errors.Is(err, domain.HookConfigInvalid) {
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
	if err = r.appendHookFacts(ctx, []session.RecordDraft{planned}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return plainFailure(tool.ResultCancelled, domain.HookCancelled)
		}
		return tool.InvocationResult{}, true, err
	}
	r.hooks.mu.Lock()
	r.hooks.planned[inv.InvocationID()] = p.Clone()
	r.hooks.mu.Unlock()
	if !r.interactiveApprovals {
		return r.finishHookGate(ctx, turn, inv, p, domain.HookBlock, domain.HookTrustRequired, 0, 0, nil)
	}
	remaining := 30 * time.Second
	for ordinal, d := range matched {
		if ctx.Err() != nil {
			return r.finishHookGate(ctx, turn, inv, p, domain.HookCancel, domain.HookCancelled, ordinal, ordinal, nil)
		}
		code, err := r.trustHook(ctx, emit, turn, inv, d)
		if err != nil {
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
		if err = r.appendHookFacts(ctx, []session.RecordDraft{started}); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return r.finishHookGate(ctx, turn, inv, p, domain.HookCancel, domain.HookCancelled, ordinal, ordinal, nil)
			}
			return tool.InvocationResult{}, true, err
		}
		r.emitHookActivity(turn, protocol.HookActivityPayload{InvocationID: string(inv.InvocationID()), Reference: d.Reference, State: "checking", Code: domain.HookOK})
		start := now()
		result := runner.Run(ctx, id, d, input, min(time.Duration(d.Reference.TimeoutSeconds)*time.Second, remaining))
		remaining -= max(0, now().Sub(start))
		if result.Validate() != nil || result.Identity != id {
			result = domain.HookRunResult{Identity: id, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "execution"}
		}
		finished, err := session.NewHookExecutionFinishedDraft(turn, result)
		if err != nil {
			return tool.InvocationResult{}, true, err
		}
		if result.Status != domain.HookContinue {
			out, blocked, err := r.finishHookGate(ctx, turn, inv, p, result.Status, result.Code, ordinal+1, ordinal, []session.RecordDraft{finished})
			if err == nil || out.Validate() == nil {
				r.emitHookActivity(turn, protocol.HookActivityPayload{InvocationID: string(inv.InvocationID()), Reference: d.Reference, State: string(result.Status), Code: result.Code, StdoutBytes: result.StdoutBytes, StderrBytes: result.StderrBytes})
			}
			return out, blocked, err
		}
		batch := []session.RecordDraft{finished}
		if ordinal == len(matched)-1 {
			decision, _ := domain.NewHookGateDecision(p, domain.HookContinue, domain.HookOK, len(matched), -1)
			gate, _ := session.NewToolHookGateDecidedDraft(turn, decision)
			batch = append(batch, gate)
		}
		if err = r.appendHookFacts(context.WithoutCancel(ctx), batch); err != nil {
			return tool.InvocationResult{}, true, err
		}
		if r.hookHistory != nil {
			r.hookHistory.HookFinished(d.Reference, result)
		}
		r.emitHookActivity(turn, protocol.HookActivityPayload{InvocationID: string(inv.InvocationID()), Reference: d.Reference, State: string(result.Status), Code: result.Code, StdoutBytes: result.StdoutBytes, StderrBytes: result.StderrBytes})
	}
	return tool.InvocationResult{}, false, nil
}

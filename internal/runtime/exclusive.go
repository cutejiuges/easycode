package runtime

import (
	"context"
	"errors"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// exclusiveStrategy 保留各工具的执行与候选关联规则，durable 骨架统一持有策略。
type exclusiveStrategy interface {
	Execute(context.Context) tool.InvocationResult
	Matches(tool.InvocationResult) bool
}

type exclusiveAction struct {
	execute func(context.Context) tool.InvocationResult
	matches func(tool.InvocationResult) bool
}

func (action exclusiveAction) Execute(ctx context.Context) tool.InvocationResult {
	return action.execute(ctx)
}
func (action exclusiveAction) Matches(result tool.InvocationResult) bool {
	return action.matches(result)
}

func buildApprovalFact(request tool.ApprovalRequest, invocation tool.Invocation, resolution approvalResolution) (session.RecordDraft, error) {
	metadata, err := session.NewApprovalMetadata(invocation)
	if err != nil {
		return session.RecordDraft{}, err
	}
	return session.NewToolApprovalDecidedDraft(request.TurnID(), session.ToolApprovalDecidedPayload{
		RequestID: request.RequestID(), InvocationID: invocation.InvocationID(), Fingerprint: request.Fingerprint(),
		Outcome: resolution.outcome, Feedback: resolution.feedback, Source: resolution.source, Metadata: metadata,
	})
}

func (runtime *Runtime) commitApprovalDecision(ctx context.Context, fact session.RecordDraft, label string) error {
	if _, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{fact}); err != nil {
		runtime.poisoned.Store(true)
		return fault.Wrap(fault.CodeSessionWrite, "persist "+label+" approval failed", err)
	}
	return nil
}

func (runtime *Runtime) commitApprovalDenial(ctx context.Context, emit Emitter, request tool.ApprovalRequest, resolution approvalResolution, fact session.RecordDraft, result tool.InvocationResult, label string) (tool.InvocationResult, bool, error) {
	result, err := runtime.freezeResult(result)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	turnID := request.TurnID()
	resultDraft, err := session.NewToolCallResultDraft(turnID, result)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	records, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{fact, resultDraft})
	if err != nil {
		runtime.poisoned.Store(true)
		return tool.InvocationResult{}, false, fault.Wrap(fault.CodeSessionWrite, "persist "+label+" denial failed", err)
	}
	if err := runtime.commitResultBudget(result); err != nil {
		runtime.poisoned.Store(true)
		return tool.InvocationResult{}, false, err
	}
	if err := runtime.notifyToolResult(turnID, result, resultRecordSequence(records)); err != nil {
		return result, false, err
	}
	runtime.emitApprovalResolved(emit, request, resolution)
	return result, resolution.outcome == tool.ApprovalOutcomeAbort, nil
}

// executeWithApproval 持久化唯一审批决定，之后由独立的 started 门禁接纳执行。
func (runtime *Runtime) executeWithApproval(ctx context.Context, emit Emitter, request tool.ApprovalRequest, invocation tool.Invocation, policy tool.PolicyDecision, label string, action exclusiveAction) (tool.InvocationResult, bool, error) {
	resolution, err := runtime.decideApproval(ctx, emit, request, policy)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	fact, err := buildApprovalFact(request, invocation, resolution)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	if resolution.outcome == tool.ApprovalOutcomeDeny || resolution.outcome == tool.ApprovalOutcomeAbort {
		return runtime.commitApprovalDenial(ctx, emit, request, resolution, fact, resolution.rejection(label, invocation), label)
	}
	if err := runtime.commitApprovalDecision(ctx, fact, label); err != nil {
		return tool.InvocationResult{}, false, err
	}
	if resolution.outcome == tool.ApprovalOutcomeAllowSession {
		runtime.approvals.grant(request.Capability()).Store(true)
	}
	runtime.emitApprovalResolved(emit, request, resolution)
	result, err := runtime.executeExclusive(ctx, request.TurnID(), invocation, action)
	return result, false, err
}

// executeExclusive 把 started 接收作为取消线性化点，返回前持久化唯一关联结果。
func (runtime *Runtime) executeExclusive(ctx context.Context, turnID domain.TurnID, invocation tool.Invocation, strategy exclusiveStrategy) (tool.InvocationResult, error) {
	finish := func(result tool.InvocationResult) (tool.InvocationResult, error) {
		if err := runtime.persistToolResult(ctx, turnID, &result); err != nil {
			return tool.InvocationResult{}, err
		}
		if result.Status() == tool.ResultOutcomeUncertain {
			return result, fault.New(fault.CodeSessionWrite, "tool execution outcome is uncertain")
		}
		return result, nil
	}
	admitted, err := runtime.admitToolExecution(ctx, turnID, invocation)
	if err != nil {
		return tool.InvocationResult{}, err
	}
	if !admitted {
		return finish(tool.NewInvocationErrorResult(invocation, tool.ResultCancelled, "cancelled_before_execution", "Tool cancelled before execution"))
	}
	result := strategy.Execute(ctx)
	if result.Validate() != nil || result.InvocationID() != invocation.InvocationID() || result.ProviderCallID() != invocation.ProviderCallID() || result.Capability() != invocation.Capability() || result.WireKind() != invocation.WireKind() || !strategy.Matches(result) {
		result = tool.NewInvocationErrorResult(invocation, tool.ResultOutcomeUncertain, "invalid_tool_result", "Tool outcome is uncertain")
	}
	if result.Status() == tool.ResultOutcomeUncertain {
		runtime.poisoned.Store(true)
		runtime.readObservations.clear()
	}
	return finish(result)
}

func (runtime *Runtime) executeApproved(ctx context.Context, emit Emitter, turnID domain.TurnID, invocation tool.Invocation) (tool.InvocationResult, bool, error) {
	if runtime.poisoned.Load() || runtime.journal.Poisoned() {
		return tool.InvocationResult{}, false, fault.New(fault.CodeSessionWrite, "session journal is poisoned")
	}
	call, sourceErr := runtime.executionCall(turnID, invocation)
	if sourceErr != nil {
		return tool.InvocationResult{}, false, sourceErr
	}
	if call.running {
		return tool.InvocationResult{}, false, fault.New(fault.CodeStreamProtocol, "tool invocation is already in flight")
	}
	if call.hasResult {
		cached, err := call.cachedResult()
		return cached, false, err
	}
	call.running = true
	defer func() { call.running = false }()
	if runtime.toolTurn.resultBudget.BodyExhausted() {
		result := tool.NewInvocationErrorResult(invocation, tool.ResultError, "result_budget_exceeded", "Tool not executed: turn result budget exhausted")
		err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result)
		return result, false, err
	}
	if result, blocked, err := runtime.checkToolHooks(ctx, emit, turnID, invocation); blocked || err != nil {
		return result, false, err
	}
	selection, selectionErr := call.selection()
	if selectionErr != nil {
		return tool.InvocationResult{}, false, selectionErr
	}
	effective, selected := selection.EffectiveInvocation()
	if !selected {
		return tool.InvocationResult{}, false, fault.New(fault.CodeStreamProtocol, "tool input was not selected")
	}
	invocation = effective
	if runtime.toolTurn.activityInvocations != nil {
		runtime.toolTurn.activityInvocations[invocation.InvocationID()] = invocation.Clone()
	}
	if ctx.Err() != nil {
		result := tool.NewInvocationErrorResult(invocation, tool.ResultCancelled, "cancelled_before_execution", "Tool cancelled before execution")
		err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result)
		return result, false, err
	}

	state, code, prepareErr := runtime.preparePostHooks(ctx, emit, turnID, invocation)
	if code != domain.HookOK || prepareErr != nil {
		if state != nil {
			state.stopCode = code
		}
		if runtime.journal.Poisoned() || runtime.poisoned.Load() {
			return tool.InvocationResult{}, false, prepareErr
		}
		status := tool.ResultError
		if code == domain.HookTrustDenied || code == domain.HookTrustRequired {
			status = tool.ResultDenied
		}
		if code == domain.HookCancelled {
			status = tool.ResultCancelled
		}
		result := tool.NewInvocationErrorResult(invocation, status, string(code), "Hook stopped: "+string(code))
		if err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result); err != nil {
			return tool.InvocationResult{}, false, err
		}
		if err := runtime.runPostHooks(ctx, turnID, invocation, result, state); err != nil {
			return result, false, err
		}
		if prepareErr != nil {
			return result, false, prepareErr
		}
		if code == domain.HookCancelled {
			return result, false, fault.Wrap(fault.CodeUserCancelled, "post hook trust cancelled", context.Canceled)
		}
		return result, false, nil
	}
	finishBeforeExecution := func(result tool.InvocationResult) (tool.InvocationResult, bool, error) {
		if err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result); err != nil {
			return tool.InvocationResult{}, false, err
		}
		if err := runtime.runPostHooks(ctx, turnID, invocation, result, state); err != nil {
			return result, false, err
		}
		return result, false, nil
	}
	if ctx.Err() != nil {
		return finishBeforeExecution(tool.NewInvocationErrorResult(invocation, tool.ResultCancelled, "cancelled_before_execution", "Tool cancelled before execution"))
	}
	allowance, allowanceErr := runtime.resultAllowance(invocation.Capability())
	if allowanceErr != nil {
		return tool.InvocationResult{}, false, allowanceErr
	}
	if _, budgetErr := tool.InputRewriteBodyAllowance(selection, allowance); budgetErr != nil {
		var exceeded tool.InputRewriteBudgetError
		if !errors.As(budgetErr, &exceeded) {
			runtime.poisoned.Store(true)
			return tool.InvocationResult{}, false, budgetErr
		}
		return finishBeforeExecution(tool.NewInvocationErrorResult(invocation, tool.ResultError, tool.HookRewriteBudgetExceeded, "Tool not executed: effective input disclosure exceeds result budget"))
	}
	if selection.Changed() {
		ready, readyErr := invocation.ReadyCall()
		if readyErr != nil {
			return tool.InvocationResult{}, false, readyErr
		}
		if runtime.tools.Policy.Decide(ready) == tool.PolicyDeny {
			return finishBeforeExecution(tool.NewInvocationErrorResult(invocation, tool.ResultError, "tool_denied", "Tool not executed: effective input is denied by policy"))
		}
	}
	var result tool.InvocationResult
	var aborted bool
	var err error
	switch invocation.Capability() {
	case tool.CapabilityPatch:
		result, aborted, err = runtime.executePatchCall(ctx, emit, turnID, invocation)
	case tool.CapabilityWrite:
		result, aborted, err = runtime.executeWriteCall(ctx, emit, turnID, invocation)
	case tool.CapabilityExec:
		result, aborted, err = runtime.executeExecCall(ctx, emit, turnID, invocation)
	default:
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "exclusive capability is unsupported")
	}
	if result.Validate() != nil || runtime.poisoned.Load() || runtime.journal.Poisoned() {
		return result, aborted, err
	}
	if postErr := runtime.runPostHooks(ctx, turnID, invocation, result, state); postErr != nil {
		return result, aborted, postErr
	}
	return result, aborted, err
}

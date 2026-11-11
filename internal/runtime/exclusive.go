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

type exclusiveResult struct {
	Result    tool.InvocationResult
	Admission tool.ExecutionAdmission
}

// executeExclusive 把 started 接收作为取消线性化点，返回前持久化唯一关联结果。
func (runtime *Runtime) executeExclusive(ctx context.Context, turnID domain.TurnID, invocation tool.Invocation, strategy exclusiveStrategy) (exclusiveResult, error) {
	var admission tool.ExecutionAdmission
	finish := func(result tool.InvocationResult) (exclusiveResult, error) {
		if err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result); err != nil {
			return exclusiveResult{}, err
		}
		if result.Status() == tool.ResultOutcomeUncertain {
			return exclusiveResult{Result: result, Admission: admission}, fault.New(fault.CodeSessionWrite, "tool execution outcome is uncertain")
		}
		return exclusiveResult{Result: result, Admission: admission}, nil
	}
	cancelled := func() (exclusiveResult, error) {
		return finish(tool.NewInvocationErrorResult(invocation, tool.ResultCancelled, "cancelled_before_execution", "Tool cancelled before execution"))
	}
	if ctx.Err() != nil {
		return cancelled()
	}
	started, err := session.NewToolExecutionStartedDraft(turnID, invocation.InvocationID())
	if err != nil {
		return exclusiveResult{}, err
	}
	records, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{started})
	if err != nil {
		if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && !runtime.journal.Poisoned() {
			return cancelled()
		}
		runtime.poisoned.Store(true)
		return exclusiveResult{}, fault.Wrap(fault.CodeSessionWrite, "persist tool execution start failed", err)
	}
	if len(records) != 1 {
		runtime.poisoned.Store(true)
		return exclusiveResult{}, fault.New(fault.CodeSessionWrite, "execution start has no durable sequence")
	}
	admission, err = tool.NewExecutionAdmission(invocation.InvocationID(), records[0].Sequence)
	if err != nil {
		runtime.poisoned.Store(true)
		return exclusiveResult{}, err
	}
	if state := runtime.hooks.post[invocation.InvocationID()]; state != nil {
		state.admission = admission
	}
	runtime.emitToolStarted(turnID, invocation)
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
	if state := runtime.hooks.post[invocation.InvocationID()]; state != nil {
		runtime.hooks.mu.Lock()
		cached, ok := runtime.hooks.completed[invocation.InvocationID()]
		runtime.hooks.mu.Unlock()
		if ok {
			ready, readyErr := session.NewToolCallReadyDraft(turnID, invocation, 0, 0)
			frozen, resultErr := session.NewToolCallResultDraft(turnID, cached)
			if readyErr != nil || resultErr != nil || domain.HookDigest(ready.PayloadBytes()) != state.invocationFingerprint || domain.HookDigest(frozen.PayloadBytes()) != state.resultFingerprint || cached.ProviderCallID() != invocation.ProviderCallID() || cached.Capability() != invocation.Capability() || cached.WireKind() != invocation.WireKind() {
				return tool.InvocationResult{}, false, fault.New(fault.CodeStreamProtocol, "post hook invocation replay mismatch")
			}
			return cached, false, runtime.runPostHooks(ctx, turnID, invocation, cached, state)
		}
	}
	if runtime.resultBudget.BodyExhausted() {
		result := tool.NewInvocationErrorResult(invocation, tool.ResultError, "result_budget_exceeded", "Tool not executed: turn result budget exhausted")
		err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result)
		return result, false, err
	}
	if result, blocked, err := runtime.checkToolHooks(ctx, emit, turnID, invocation); blocked || err != nil {
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

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

// executeExclusive 把 started 接收作为取消线性化点，返回前持久化唯一关联结果。
func (runtime *Runtime) executeExclusive(ctx context.Context, turnID domain.TurnID, invocation tool.Invocation, strategy exclusiveStrategy) (tool.InvocationResult, error) {
	finish := func(result tool.InvocationResult) (tool.InvocationResult, error) {
		if err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result); err != nil {
			return tool.InvocationResult{}, err
		}
		if result.Status() == tool.ResultOutcomeUncertain {
			return result, fault.New(fault.CodeSessionWrite, "tool execution outcome is uncertain")
		}
		return result, nil
	}
	cancelled := func() (tool.InvocationResult, error) {
		return finish(tool.NewInvocationErrorResult(invocation, tool.ResultCancelled, "cancelled_before_execution", "Tool cancelled before execution"))
	}
	if ctx.Err() != nil {
		return cancelled()
	}
	started, err := session.NewToolExecutionStartedDraft(turnID, invocation.InvocationID())
	if err != nil {
		return tool.InvocationResult{}, err
	}
	if _, err = runtime.journal.AppendBatch(ctx, []session.RecordDraft{started}); err != nil {
		if (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) && !runtime.journal.Poisoned() {
			return cancelled()
		}
		runtime.poisoned.Store(true)
		return tool.InvocationResult{}, fault.Wrap(fault.CodeSessionWrite, "persist tool execution start failed", err)
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
	if runtime.resultBudget.BodyExhausted() {
		result := tool.NewInvocationErrorResult(invocation, tool.ResultError, "result_budget_exceeded", "Tool not executed: turn result budget exhausted")
		err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result)
		return result, false, err
	}
	switch invocation.Capability() {
	case tool.CapabilityPatch:
		return runtime.executePatchCall(ctx, emit, turnID, invocation)
	case tool.CapabilityWrite:
		return runtime.executeWriteCall(ctx, emit, turnID, invocation)
	case tool.CapabilityExec:
		return runtime.executeExecCall(ctx, emit, turnID, invocation)
	default:
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "exclusive capability is unsupported")
	}
}

package runtime

import (
	"context"
	"fmt"
	"sync"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// taskProgressProjection 只持有本线程最近成功事实，不执行 I/O 或用户回调。
type taskProgressProjection struct {
	mu    sync.RWMutex
	state domain.TaskProgressState
}

// TaskProgress 返回当前已确定提交的任务副本，允许活动回合中的只读查询。
func (r *Runtime) TaskProgress() domain.TaskProgressState {
	r.tasks.mu.RLock()
	state := r.tasks.state.Clone()
	r.tasks.mu.RUnlock()
	return state
}

func resultRecordSequence(records []session.Record) uint64 {
	if len(records) == 0 {
		return 0
	}
	return records[len(records)-1].Sequence
}

func (r *Runtime) installTaskProgress(turn domain.TurnID, result tool.InvocationResult, sequence uint64) error {
	if result.Capability() != tool.CapabilityTaskProgress {
		return nil
	}
	update, applied := result.TaskUpdate()
	if !applied {
		return nil
	}
	source, err := domain.NewTaskProgressSource(sequence, turn, string(result.InvocationID()))
	if err != nil {
		r.poisoned.Store(true)
		return err
	}
	state, err := domain.NewTaskProgressState(source, update)
	if err != nil {
		r.poisoned.Store(true)
		return err
	}
	r.tasks.mu.Lock()
	previous, exists := r.tasks.state.Source()
	if exists && previous.ResultSequence() >= sequence {
		identical := r.tasks.state.Equal(state)
		r.tasks.mu.Unlock()
		if identical {
			return nil
		}
		r.poisoned.Store(true)
		return fault.New(fault.CodeStreamProtocol, "task result sequence did not advance")
	}
	r.tasks.state = state
	r.tasks.mu.Unlock()
	event, err := protocol.NewTaskProgressUpdated(r.sessionID, r.threadID, turn, state)
	if err != nil {
		r.poisoned.Store(true)
		return err
	}
	if r.toolTurn.activityEmitter != nil {
		r.toolTurn.activityEmitter(event)
	}
	return nil
}

func (r *Runtime) executeTaskCall(ctx context.Context, turn domain.TurnID, inv tool.Invocation) (tool.InvocationResult, bool, error) {
	call, err := r.executionCall(turn, inv)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	if call.hasResult {
		result, err := call.cachedResult()
		return result, false, err
	}
	if call.running {
		return tool.InvocationResult{}, false, fault.New(fault.CodeStreamProtocol, "task invocation is already running")
	}
	call.running = true
	defer func() { call.running = false }()
	fail := func(status tool.ResultStatus, code, message string) (tool.InvocationResult, bool, error) {
		result := tool.NewInvocationErrorResult(inv, status, code, message)
		err := r.persistToolResult(context.WithoutCancel(ctx), turn, &result)
		return result, false, err
	}
	if r.toolTurn.resultBudget.BodyExhausted() {
		return fail(tool.ResultError, "result_budget_exceeded", "Task not updated: turn result budget exhausted")
	}
	ready, err := inv.ReadyCall()
	if err != nil || r.tools.Policy.Decide(ready) != tool.PolicyAllow {
		return fail(tool.ResultDenied, "permission_denied", "Task updates are not allowed")
	}
	allowance, err := r.resultAllowance(inv.Capability())
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	preparer := r.providerOperations.toolOutputs
	if preparer == nil {
		return tool.InvocationResult{}, false, fmt.Errorf("task output preparation is unavailable")
	}
	preflight, err := preparer.PrepareToolOutputPreflight(inv)
	if err != nil || preflight == nil {
		return tool.InvocationResult{}, false, fmt.Errorf("task output preflight is unavailable")
	}
	v, _ := inv.Task()
	result, err := r.executeExclusive(ctx, turn, inv, exclusiveAction{
		execute: func(ctx context.Context) tool.InvocationResult {
			result := r.tools.Task.Execute(ctx, v)
			if result.Status() != tool.ResultSuccess {
				return result
			}
			frozen, freezeErr := tool.FinalizeResult(result, allowance, tool.BudgetPreserved)
			if freezeErr != nil || session.PreflightTaskResult(session.Identity{SessionID: r.sessionID, ThreadID: r.threadID}, turn, frozen) != nil {
				return tool.NewInvocationErrorResult(inv, tool.ResultError, "result_budget_exceeded", "Task not updated: complete result exceeds limits")
			}
			evidence, evidenceErr := r.resultEvidence(frozen)
			if evidenceErr != nil {
				return tool.InvocationResult{}
			}
			output, outputErr := tool.NewModelOutput(frozen, evidence)
			if outputErr != nil || preflight.Validate(output) != nil {
				return tool.NewInvocationErrorResult(inv, tool.ResultError, "result_budget_exceeded", "Task not updated: encoded output exceeds limits")
			}
			return result
		},
		matches: func(result tool.InvocationResult) bool { return tool.ValidateResultInput(inv, result) == nil },
	})
	return result, false, err
}

func (state *agentLoopRunState) queryTasks(request loopCommandRequest) {
	r := state.loop.runtime
	event, err := protocol.NewTaskProgressSnapshot(r.sessionID, r.threadID, r.TaskProgress())
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	result, err := protocol.NewAcceptedCommandResult(request.command, protocol.CommandDispositionQueried)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	item, err := protocol.NewCorrelatedTaskProgressItem(request.command.RequestID(), event)
	if err != nil {
		state.reject(request, protocol.ControlErrorInvalidCommand, true)
		return
	}
	state.emitResult(request, result)
	state.loop.outputs <- item
	state.completeRequest(request.command.RequestID())
}

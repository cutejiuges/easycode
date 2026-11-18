package runtime

import (
	"context"
	"errors"

	"testing"

	"easycode/internal/domain"

	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestTaskTurnBudgetExhaustionPairsFailureWithoutApplyingOrExecuting(t *testing.T) {
	j := &fakeJournal{}
	r := taskRuntimeFixture(t, j)
	executor := &taskCountingExecutor{}
	r.taskExecutor = executor
	prior := taskRuntimeInvocation(t, "此前成功清单", domain.TaskCompleted, "prior")
	if err := r.installTaskProgress(runtimeTurnID, sessionTaskCandidateForBoundary(t, prior), 10); err != nil {
		t.Fatal(err)
	}
	saved := r.TaskProgress()
	// 使用真实 turn 账本扣满正文额度，不伪造 exhausted 标志。
	for !r.resultBudget.BodyExhausted() {
		if err := r.resultBudget.BeginSample(1); err != nil {
			t.Fatal(err)
		}
		allowance, err := r.resultBudget.Allowance(tool.CapabilityRead)
		if err != nil {
			t.Fatal(err)
		}
		metadata, _ := tool.NewResultBudgetMetadata(allowance, allowance, tool.BudgetPreserved)
		if err := r.resultBudget.Commit(tool.CapabilityRead, metadata); err != nil {
			t.Fatal(err)
		}
	}
	used := r.resultBudget.TurnUsed()
	inv := taskRuntimeInvocation(t, "不得应用的候选", domain.TaskPending, "exhausted-task")
	results, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{inv})
	if err != nil || len(results) != 1 || results[0].Status() != tool.ResultError || results[0].Code() != "result_budget_exceeded" {
		t.Fatal("exhausted task did not produce a paired budget failure", err)
	}
	if _, applied := results[0].TaskUpdate(); applied || !r.TaskProgress().Equal(saved) || executor.calls.Load() != 0 || r.poisoned.Load() {
		t.Fatal("exhausted task executed or applied its candidate")
	}
	if r.resultTicket != nil || r.resultBudget.TurnUsed() != used+results[0].Budget().ActualBytes() || !r.resultBudget.BodyExhausted() {
		t.Fatal("exhausted task was rebudgeted or charged more than once")
	}
	if err := r.commitToolOutputs(context.Background(), runtimeTurnID, results); err != nil {
		t.Fatal("exhausted task output lost its pairing", err)
	}
	conversation := r.conversation.(*fakeToolConversation)
	if len(conversation.results) != 1 || conversation.results[0].ProviderCallID() != inv.ProviderCallID() || conversation.results[0].Preview() != results[0].Preview() {
		t.Fatal("budget failure native output changed identity or frozen text")
	}
	for _, batch := range j.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventToolExecutionStarted {
				t.Fatal("exhausted task crossed execution admission")
			}
		}
	}
}

// sessionTaskCandidateForBoundary 只构造工具候选，不声称对应结果已经落盘。
func sessionTaskCandidateForBoundary(t *testing.T, inv tool.Invocation) tool.InvocationResult {
	t.Helper()
	v, _ := inv.Task()
	result, err := tool.NewTaskInvocationResult(v)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type taskRejectingOutputConversation struct {
	*fakeToolConversation
	checks int
}

type taskRejectingOutputPreflight struct {
	owner *taskRejectingOutputConversation
}

func (p taskRejectingOutputPreflight) Validate(output tool.ModelOutput) error {
	p.owner.checks++
	if output.Validate() != nil || output.Result().Status() != tool.ResultSuccess {
		return errors.New("invalid task preflight candidate")
	}
	return errors.New("injected encoded output limit")
}

func (c *taskRejectingOutputConversation) PrepareToolOutputPreflight(inv tool.Invocation) (provider.ToolOutputPreflight, error) {
	if inv.Validate() != nil || inv.Capability() != tool.CapabilityTaskProgress {
		return nil, errors.New("invalid task preflight invocation")
	}
	return taskRejectingOutputPreflight{c}, nil
}

func TestTaskOutputPreflightRejectionPairsBudgetFailureAndKeepsPriorState(t *testing.T) {
	j := &fakeJournal{}
	conversation := &taskRejectingOutputConversation{fakeToolConversation: &fakeToolConversation{fakeConversation: &fakeConversation{}}}
	executor := &taskCountingExecutor{}
	config := testRuntimeConfig(t, j)
	config.TaskExecutor = executor
	r, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	prior := taskRuntimeInvocation(t, "先前成功清单", domain.TaskCompleted, "prior")
	if err := r.installTaskProgress(runtimeTurnID, sessionTaskCandidateForBoundary(t, prior), 10); err != nil {
		t.Fatal(err)
	}
	saved := r.TaskProgress()
	inv := taskRuntimeInvocation(t, "候选不能部分成功", domain.TaskInProgress, "rejected-output")
	results, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{inv})
	if err != nil || len(results) != 1 || results[0].Status() != tool.ResultError || results[0].Code() != "result_budget_exceeded" || !r.TaskProgress().Equal(saved) {
		t.Fatal("output encoding failure applied a partial success", err)
	}
	if conversation.checks != 1 || executor.calls.Load() != 1 || r.poisoned.Load() || r.resultTicket != nil || r.resultBudget.TurnUsed() != results[0].Budget().ActualBytes() {
		t.Fatal("preflight failure repeated execution or budget commitment")
	}
	if _, applied := results[0].TaskUpdate(); applied || results[0].TaskInput().IsZero() {
		t.Fatal("preflight failure lost input or retained success metadata")
	}
	if err := r.commitToolOutputs(context.Background(), runtimeTurnID, results); err != nil {
		t.Fatal("preflight failure could not be paired", err)
	}
	if len(conversation.results) != 1 || conversation.results[0].Code() != "result_budget_exceeded" || conversation.results[0].ProviderCallID() != inv.ProviderCallID() {
		t.Fatal("preflight failure native pair changed")
	}
	started, terminals := 0, 0
	for _, batch := range j.snapshot() {
		for _, draft := range batch {
			switch draft.EventKind() {
			case session.EventToolExecutionStarted:
				started++
			case session.EventToolCallResult:
				terminals++
				payload, err := session.DecodeToolCallResultPayload(session.Record{EventKind: draft.EventKind(), Payload: draft.PayloadBytes()})
				if err != nil || payload.Status != tool.ResultError || payload.TaskMetadata.Update != nil {
					t.Fatal("encoded failure contains applied task metadata", err)
				}
			}
		}
	}
	if started != 1 || terminals != 1 {
		t.Fatal("preflight rejection duplicated admission or result")
	}
}

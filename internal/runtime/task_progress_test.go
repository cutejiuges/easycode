package runtime

import (
	"context"
	"sync/atomic"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
)

func taskRuntimeFixture(t *testing.T, journal Journal) *Runtime {
	t.Helper()
	c := &fakeToolConversation{fakeConversation: &fakeConversation{}}
	config := testRuntimeConfig(t, journal)
	config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Task = builtin.NewTaskExecutor() })
	r, err := New(c, config)
	if err != nil {
		t.Fatal(err)
	}
	r.toolTurn.resultBudget = tool.NewResultBudget()
	return r
}

func taskRuntimeInvocation(t *testing.T, label string, status domain.TaskStatus, call string) tool.Invocation {
	t.Helper()
	var items []domain.TaskStep
	if label != "" {
		step, err := domain.NewTaskStep(label, "", status)
		if err != nil {
			t.Fatal(err)
		}
		items = []domain.TaskStep{step}
	}
	update, err := domain.NewTaskUpdate(items, "进度说明")
	if err != nil {
		t.Fatal(err)
	}
	input, err := tool.NewTaskInput(tool.TaskProfileUpdatePlan, update)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := tool.NewTaskReadyCall(tool.ProviderCallID(call), input)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := tool.GenerateInvocationID()
	inv, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

// taskAdmissionJournal 用明确接收边界注入取消，避免依赖调度概率。
type taskAdmissionJournal struct {
	fakeJournal
	before  bool
	entered chan struct{}
	release chan struct{}
	gated   atomic.Bool
	runtime *Runtime
	tickets []*tool.ResultBudgetTicket
}

func (j *taskAdmissionJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	if j.runtime != nil && len(drafts) == 1 && drafts[0].EventKind() == session.EventToolCallResult {
		j.tickets = append(j.tickets, j.runtime.toolTurn.resultTicket)
	}

	if len(drafts) == 1 && drafts[0].EventKind() == session.EventToolCallResult && j.gated.CompareAndSwap(false, true) {
		close(j.entered)
		<-j.release
		if j.before && ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return j.fakeJournal.AppendBatch(context.WithoutCancel(ctx), drafts)
}

func TestTaskResultAdmissionCancellationAndSyncProjection(t *testing.T) {
	for _, before := range []bool{true, false} {
		name := "after_acceptance"
		if before {
			name = "before_acceptance"
		}
		t.Run(name, func(t *testing.T) {
			j := &taskAdmissionJournal{before: before, entered: make(chan struct{}), release: make(chan struct{})}
			r := taskRuntimeFixture(t, j)
			j.runtime = r
			if err := r.toolTurn.resultBudget.BeginSample(1); err != nil {
				t.Fatal(err)
			}
			inv := taskRuntimeInvocation(t, "验证结果", domain.TaskInProgress, "task")
			seedRuntimeToolCall(t, r, runtimeTurnID, inv, 0, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type outcome struct {
				result tool.InvocationResult
				err    error
			}
			done := make(chan outcome, 1)
			var notifications atomic.Int32
			r.toolTurn.activityEmitter = func(e protocol.Event) {
				if e.Kind == protocol.EventTaskProgressUpdated {
					if r.TaskProgress().Kind() != domain.TaskProgressSnapshot {
						t.Error("notification before projection")
					}
					notifications.Add(1)
				}
			}
			go func() { result, _, err := r.executeTaskCall(ctx, runtimeTurnID, inv); done <- outcome{result, err} }()
			<-j.entered
			if r.TaskProgress().Kind() != domain.TaskProgressNone {
				t.Fatal("candidate escaped before Sync")
			}
			cancel()
			select {
			case <-done:
				t.Fatal("owner abandoned pending resolution")
			default:
			}
			close(j.release)
			got := <-done
			if got.err != nil || r.poisoned.Load() || j.Poisoned() {
				t.Fatalf("normal cancellation poisoned: %v", got.err)
			}
			want := tool.ResultSuccess
			if before {
				want = tool.ResultCancelled
			}
			if got.result.Status() != want || r.toolTurn.resultTicket != nil || r.toolTurn.resultBudget.BodyExhausted() || r.toolTurn.resultBudget.TurnUsed() != got.result.Budget().ActualBytes() {
				t.Fatalf("result/ticket mismatch: %s", got.result.Status())
			}
			batches := j.snapshot()
			if len(batches) != 2 || batches[0][0].EventKind() != session.EventToolExecutionStarted || batches[1][0].EventKind() != session.EventToolCallResult {
				t.Fatal("duplicate result or admission")
			}
			if before {
				if len(j.tickets) != 2 || j.tickets[0] == nil || j.tickets[0] != j.tickets[1] {
					t.Fatal("cancelled result did not reuse its sole ticket")
				}
				if r.TaskProgress().Kind() != domain.TaskProgressNone || notifications.Load() != 0 {
					t.Fatal("cancelled candidate applied")
				}
			} else {
				state := r.TaskProgress()
				source, _ := state.Source()
				if source.ResultSequence() != 2 || source.InvocationID() != string(inv.InvocationID()) || notifications.Load() != 1 {
					t.Fatal("source did not use actual synced result")
				}
				// 同一调用重入只返回冻结结果，不增加记录或再次应用。
				again, _, err := r.executeTaskCall(context.Background(), runtimeTurnID, inv)
				if err != nil || again.Status() != tool.ResultSuccess || len(j.snapshot()) != 2 || notifications.Load() != 1 {
					t.Fatal("durable success was repeated", err)
				}
			}
		})
	}
}

func TestTaskResultFailuresRetainPriorStateAndStopAdmission(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		j := &fakeJournal{failAt: failAt}
		r := taskRuntimeFixture(t, j)
		if err := r.toolTurn.resultBudget.BeginSample(1); err != nil {
			t.Fatal(err)
		}
		old := taskRuntimeInvocation(t, "原清单", domain.TaskCompleted, "old")
		previous, _ := old.Task()
		result, _ := tool.NewTaskInvocationResult(previous)
		if err := r.installTaskProgress(runtimeTurnID, result, 10); err != nil {
			t.Fatal(err)
		}
		inv := taskRuntimeInvocation(t, "候选清单", domain.TaskPending, "new")
		seedRuntimeToolCall(t, r, runtimeTurnID, inv, 0, 0)
		_, _, err := r.executeTaskCall(context.Background(), runtimeTurnID, inv)
		state := r.TaskProgress()
		source, _ := state.Source()
		if err == nil || !r.poisoned.Load() || source.ResultSequence() != 10 {
			t.Fatal("uncertain write applied candidate", err)
		}
		if _, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{inv}); err == nil {
			t.Fatal("poisoned owner admitted more work")
		}
	}
}

func TestTaskReplacementRegressionClearAndOrderedReadBarrier(t *testing.T) {
	j := &fakeJournal{}
	r := taskRuntimeFixture(t, j)
	readInput, _ := tool.NewReadInput("a.go", 1, 10)
	readCall, _ := tool.NewReadReadyCall("read-before", readInput)
	id, _ := tool.GenerateInvocationID()
	read, _ := tool.NewInvocation(id, readCall)
	task := taskRuntimeInvocation(t, "检查代码", domain.TaskCompleted, "task-complete")
	afterCall, _ := tool.NewReadReadyCall("read-after", readInput)
	id, _ = tool.GenerateInvocationID()
	after, _ := tool.NewInvocation(id, afterCall)
	results, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{read, task, after})
	if err != nil || len(results) != 3 {
		t.Fatal("ordered sample failed", err)
	}
	var resultsOrder []tool.InvocationID
	for _, batch := range j.snapshot() {
		for _, d := range batch {
			if d.EventKind() == session.EventToolCallResult {
				var payload session.ToolCallResultPayload
				if err := codec.UnmarshalRecord(d.PayloadBytes(), &payload); err != nil {
					t.Fatal(err)
				}
				resultsOrder = append(resultsOrder, payload.InvocationID)
			}
		}
	}
	if len(resultsOrder) != 3 || resultsOrder[0] != read.InvocationID() || resultsOrder[1] != task.InvocationID() || resultsOrder[2] != after.InvocationID() {
		t.Fatal("exclusive task crossed read barrier", resultsOrder)
	}
	if r.TaskProgress().Kind() != domain.TaskProgressSnapshot {
		t.Fatal("all completed deleted list")
	}
	for i, inv := range []tool.Invocation{taskRuntimeInvocation(t, "检查代码", domain.TaskPending, "regress"), taskRuntimeInvocation(t, "", domain.TaskPending, "clear")} {
		if _, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{inv}); err != nil {
			t.Fatal(err)
		}
		update, _ := r.TaskProgress().Update()
		if i == 0 && update.Items()[0].Status() != domain.TaskPending {
			t.Fatal("valid regression rejected")
		}
	}
	if r.TaskProgress().Kind() != domain.TaskProgressClear {
		t.Fatal("clear lost its durable identity")
	}
	if _, err := New(&fakeConversation{}, Config{}); err == nil {
		t.Fatal("invalid constructor accepted")
	}
}

func TestTaskQueryIsCoherentWhileResultSyncIsPendingAndDoesNoIO(t *testing.T) {
	j := &taskAdmissionJournal{entered: make(chan struct{}), release: make(chan struct{})}
	r := taskRuntimeFixture(t, j)
	if err := r.toolTurn.resultBudget.BeginSample(1); err != nil {
		t.Fatal(err)
	}
	inv := taskRuntimeInvocation(t, "等待落盘", domain.TaskInProgress, "pending")
	seedRuntimeToolCall(t, r, runtimeTurnID, inv, 0, 0)
	done := make(chan error, 1)
	go func() { _, _, err := r.executeTaskCall(context.Background(), runtimeTurnID, inv); done <- err }()
	<-j.entered
	loop, err := NewAgentLoop(r, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	state := newAgentLoopRunState(loop, context.Background())
	query := func(id protocol.RequestID, want domain.TaskProgressKind) {
		t.Helper()
		before := len(j.snapshot())
		command, _ := protocol.NewQueryTasksCommand(id)
		reply := make(chan loopCommandResponse, 1)
		state.handleCommand(loopCommandRequest{command: command, result: reply})
		got := <-reply
		if got.err != nil || got.result.Disposition() != protocol.CommandDispositionQueried {
			t.Fatal("query admission", got.err)
		}
		first := <-loop.outputs
		second := <-loop.outputs
		response, ok := first.CommandResult()
		if !ok || response.RequestID() != id {
			t.Fatal("query response did not precede snapshot")
		}
		event, ok := second.ContextEvent()
		payload, err := protocol.DecodeTaskProgressSnapshot(event)
		if !ok || err != nil || payload.Progress.Kind() != want || event.TurnID != "" || event.CompactionID != "" || second.RequestID() != id {
			t.Fatal("query coherent scope", err)
		}
		if len(j.snapshot()) != before {
			t.Fatal("query appended journal")
		}
	}
	query("pending", domain.TaskProgressNone)
	close(j.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	query("committed", domain.TaskProgressSnapshot)
}

// taskCountingExecutor 记录任何恢复期间错误的重新执行。
type taskCountingExecutor struct{ calls atomic.Int32 }

func (e *taskCountingExecutor) Execute(ctx context.Context, inv tool.TaskInvocation) tool.InvocationResult {
	e.calls.Add(1)
	return builtin.NewTaskExecutor().Execute(ctx, inv)
}

func TestTaskSuccessRemainsAppliedAfterNativeOutputSyncFailure(t *testing.T) {
	j := &fakeJournal{failAt: 3}
	r := taskRuntimeFixture(t, j)
	inv := taskRuntimeInvocation(t, "先保存清单", domain.TaskPending, "task")
	results, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{inv})
	if err != nil {
		t.Fatal(err)
	}
	before := r.TaskProgress()
	if before.Kind() != domain.TaskProgressSnapshot {
		t.Fatal("task waited for native outputs")
	}
	if err := r.commitToolOutputs(context.Background(), runtimeTurnID, results); err == nil || !r.poisoned.Load() {
		t.Fatal("native Sync failure was not fatal")
	}
	if !r.TaskProgress().Equal(before) || len(j.snapshot()) != 2 {
		t.Fatal("native output failure rolled back durable task")
	}
}

func TestTaskIsExclusiveWithoutApprovalHookOrGrantAuthority(t *testing.T) {
	j := &fakeJournal{}
	r, _, runner, _ := hookRuntimeFixture(t, j, 1)
	// 现有 mutation Hook 故意未信任；任务仍不得触发它或普通批准。
	inv := taskRuntimeInvocation(t, "仅报告进度", domain.TaskCompleted, "task")
	var unexpected int
	results, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{inv}, func(e protocol.Event) {
		if e.Kind == protocol.EventToolApprovalRequested || e.Kind == protocol.EventHookTrustRequested || e.Kind == protocol.EventHookActivity {
			unexpected++
		}
	})
	if err != nil || len(results) != 1 || results[0].Status() != tool.ResultSuccess || runner.runs != 0 || runner.preflights != 0 || unexpected != 0 || r.approvals.patchGrant.Load() || r.approvals.writeGrant.Load() {
		t.Fatal("task acquired mutation authority", err)
	}
	if r.TaskProgress().Counts().Completed != 1 {
		t.Fatal("task not committed")
	}
	denied := taskRuntimeInvocation(t, "拒绝候选", domain.TaskPending, "denied")
	before := r.TaskProgress()
	r.tools.Policy = taskDenyPolicy{}
	results, err = executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{denied})
	if err != nil || len(results) != 1 || results[0].Status() != tool.ResultDenied || !r.TaskProgress().Equal(before) {
		t.Fatal("denied candidate applied", err)
	}
	started := 0
	for _, batch := range j.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventToolExecutionStarted {
				started++
			}
		}
	}
	if started != 1 {
		t.Fatal("denied task crossed execution admission")
	}
}

// taskDenyPolicy 只为任务拒绝路径提供明确策略。
type taskDenyPolicy struct{}

func (taskDenyPolicy) Decide(tool.ReadyCall) tool.PolicyDecision { return tool.PolicyDeny }

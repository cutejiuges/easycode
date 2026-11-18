package runtime

import (
	"context"
	"fmt"
	"testing"

	"easycode/internal/session"
	"easycode/internal/tool"
)

type fixedWritePreparer struct {
	candidate tool.WriteCandidate
	calls     int
	onPrepare func(tool.ReadObservation)
}

func (p *fixedWritePreparer) Prepare(_ context.Context, _ tool.WriteInvocation, o tool.ReadObservation) (tool.WriteCandidate, error) {
	p.calls++
	if p.onPrepare != nil {
		p.onPrepare(o)
	}
	return p.candidate, nil
}

type fixedWriteExecutor struct {
	calls          int
	onExecute      func(context.Context)
	status         tool.ResultStatus
	resultOverride func(tool.WriteCandidate) tool.InvocationResult
}

func (e *fixedWriteExecutor) Execute(ctx context.Context, c tool.WriteCandidate) tool.InvocationResult {
	e.calls++
	if e.resultOverride != nil {
		return e.resultOverride(c)
	}
	if ctx.Err() != nil {
		return tool.NewWriteCandidateResult(c, tool.ResultCancelled, "cancelled")
	}
	if e.onExecute != nil {
		e.onExecute(ctx)
	}
	if e.status == tool.ResultConflict {
		return tool.NewWriteCandidateResult(c, e.status, "write_conflict")
	}
	if e.status == tool.ResultOutcomeUncertain {
		return tool.NewWriteCandidateResult(c, e.status, "write_durability_uncertain")
	}
	return tool.NewWriteCandidateResult(c, tool.ResultSuccess, "ok")
}

func runtimeWriteInvocation(t *testing.T) (tool.Invocation, tool.WriteCandidate) {
	t.Helper()
	input, _ := tool.NewWriteInput("new-file", "new")
	ready, _ := tool.NewWriteReadyCall("call-write", input)
	id, _ := tool.GenerateInvocationID()
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := invocation.Write()
	root, _ := tool.NewDirectoryEvidence(".", 1, 2, 0o700)
	candidate, err := tool.NewWriteCandidate(v, tool.WriteCreate, tool.FileBaseEvidence{}, []tool.DirectoryEvidence{root}, nil, "+new\n")
	if err != nil {
		t.Fatal(err)
	}
	return invocation, candidate
}

func writeTestRuntime(t *testing.T, journal Journal, c tool.WriteCandidate, e tool.WriteExecutor) *Runtime {
	t.Helper()
	r := newTestRuntime(t, &fakeToolConversation{fakeConversation: &fakeConversation{}}, journal)
	r.writePreparer = &fixedWritePreparer{candidate: c}
	r.writeExecutor = e
	r.interactiveApprovals = true
	return r
}

func TestWriteAcceptedStartedCallsExecutorOnceEvenAfterCancellation(t *testing.T) {
	for _, boundary := range []string{"before-start", "after-start", "after-publish"} {
		t.Run(boundary, func(t *testing.T) {
			invocation, c := runtimeWriteInvocation(t)
			journal := &fakeJournal{}
			executor := &fixedWriteExecutor{}
			runtime := writeTestRuntime(t, journal, c, executor)
			runtime.toolPolicy = allowPatchPolicy{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			journal.hook = func(batch []session.RecordDraft) {
				kind := batch[0].EventKind()
				if boundary == "before-start" && kind == session.EventToolApprovalDecided || boundary == "after-start" && kind == session.EventToolExecutionStarted {
					cancel()
				}
			}
			executor.onExecute = func(ctx context.Context) {
				if boundary == "after-start" && ctx.Err() == nil {
					t.Error("已接收 executor 未见取消")
				}
				if boundary == "after-publish" {
					cancel()
				}
			}
			results, err := executeReadyFixtureCalls(t, runtime, ctx, runtimeTurnID, []tool.Invocation{invocation})
			if err != nil {
				t.Fatal(err)
			}
			if boundary == "before-start" {
				if executor.calls != 0 || results[0].Status() != tool.ResultCancelled {
					t.Fatal("取消仍准入执行")
				}
			} else if boundary == "after-start" {
				if executor.calls != 1 || results[0].Status() != tool.ResultCancelled {
					t.Fatal("接收后未调用 executor 返回确定取消")
				}
			} else if executor.calls != 1 || results[0].Status() != tool.ResultSuccess {
				t.Fatal("接收后未执行一次或覆盖成功结果")
			}
		})
	}
}

func TestWriteUncertainStopsSiblingsButPersistsFrozenOutput(t *testing.T) {
	invocation, c := runtimeWriteInvocation(t)
	journal := &fakeJournal{}
	executor := &fixedWriteExecutor{status: tool.ResultOutcomeUncertain}
	runtime := writeTestRuntime(t, journal, c, executor)
	runtime.toolPolicy = allowPatchPolicy{}
	reader := &recordingReadExecutor{}
	runtime.readExecutor = reader
	read := testRuntimeInvocations(t, 1)[0]
	results, err := executeReadyFixtureCalls(t, runtime, context.Background(), runtimeTurnID, []tool.Invocation{invocation, read})
	if err == nil || len(results) != 2 || !runtime.poisoned.Load() || reader.calls.Load() != 0 || results[0].Status() != tool.ResultOutcomeUncertain || results[1].Status() != tool.ResultCancelled {
		t.Fatal("uncertain 未停止后续执行", err)
	}
	if err := runtime.commitToolOutputs(context.Background(), runtimeTurnID, results); err != nil {
		t.Fatal("健康 journal 未保存配对 output", err)
	}
	if runtime.conversation.(*fakeToolConversation).prepareCalls.Load() != 1 {
		t.Fatal("未形成配对输出")
	}
}

func TestWriteRecoveryNeverCallsPreparerOrExecutor(t *testing.T) {
	for _, state := range []session.ReplayedToolCallState{session.ReplayedToolCallReady, session.ReplayedToolCallStarted, session.ReplayedToolCallResult} {
		for _, approved := range []bool{false, true} {
			if state != session.ReplayedToolCallReady && !approved {
				continue
			}
			t.Run(string(state)+fmt.Sprint(approved), func(t *testing.T) {
				invocation, c := runtimeWriteInvocation(t)
				call := session.ReplayedToolCall{Origin: runtimeFixtureOrigin(t, 0, 0), Invocation: invocation, State: state, ReadySequence: 2}
				if approved {
					call.ApprovalSequence = 3
					call.ApprovalOutcome = tool.ApprovalOutcomeAllowSession
					call.CandidateFingerprint = c.Fingerprint()
				}
				if state != session.ReplayedToolCallReady {
					call.StartedSequence = 4
				}
				if state == session.ReplayedToolCallResult {
					call.ResultSequence = 5
					call.Result = tool.NewWriteCandidateResult(c, tool.ResultSuccess, "ok")
				}
				plan, err := session.NewToolRecoveryPlan(runtimeTurnID, 1, []session.ReplayedToolCall{call}, false)
				if err != nil {
					t.Fatal(err)
				}
				executor := &fixedWriteExecutor{}
				runtime := writeTestRuntime(t, &fakeJournal{}, c, executor)
				if err := runtime.ReconcileToolTurn(context.Background(), plan); err != nil {
					t.Fatal(err)
				}
				if executor.calls != 0 || runtime.writePreparer.(*fixedWritePreparer).calls != 0 || runtime.approvals.writeGrant.Load() || len(runtime.readObservations.current) != 0 {
					t.Fatal("恢复访问工作区或恢复授权")
				}
				result := runtime.conversation.(*fakeToolConversation).results[0]
				want := "session_interrupted_before_execution"
				if !approved {
					want = "session_interrupted_before_approval"
				}
				if state == session.ReplayedToolCallStarted {
					want = "outcome_uncertain"
				}
				if state == session.ReplayedToolCallResult {
					want = "ok"
					if result.Preview() != call.Result.Preview() {
						t.Fatal("冻结 preview 改变")
					}
				}
				if result.Code() != want {
					t.Fatal("补偿状态错误", result.Code())
				}
			})
		}
	}
}

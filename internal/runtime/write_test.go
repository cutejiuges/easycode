package runtime

import (
	"context"
	"fmt"
	"testing"

	"easycode/internal/protocol"
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
	calls     int
	onExecute func(context.Context)
	status    tool.ResultStatus
}

func (e *fixedWriteExecutor) Execute(ctx context.Context, c tool.WriteCandidate) tool.InvocationResult {
	e.calls++
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

func approveWrite(t *testing.T, runtime *Runtime, decision tool.ApprovalDecision) Emitter {
	t.Helper()
	return func(event protocol.Event) {
		if event.Kind != protocol.EventToolApprovalRequested {
			return
		}
		p, err := protocol.DecodeToolApprovalRequested(event)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := tool.ParseApprovalRequestID(p.RequestID)
		invocation, _ := tool.ParseInvocationID(p.InvocationID)
		fingerprint, _ := tool.ParseCandidateFingerprint(p.Fingerprint)
		response, err := tool.NewApprovalResponse(id, event.TurnID, invocation, fingerprint, decision, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.ResolveToolApproval(response); err != nil {
			t.Fatal(err)
		}
		if err := runtime.ResolveToolApproval(response); err == nil {
			t.Fatal("重复响应被接受")
		}
	}
}

func TestWriteGrantAndExecutionRespectEveryDurableBoundary(t *testing.T) {
	for _, failAt := range []int{0, 1, 2, 3} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			invocation, c := runtimeWriteInvocation(t)
			journal := &fakeJournal{failAt: failAt}
			executor := &fixedWriteExecutor{}
			runtime := writeTestRuntime(t, journal, c, executor)
			journal.hook = func(batch []session.RecordDraft) {
				if batch[0].EventKind() == session.EventToolApprovalDecided && runtime.writeGrant.Load() {
					t.Error("allow Sync 返回前安装 grant")
				}
			}
			executor.onExecute = func(context.Context) {
				batches := journal.snapshot()
				if len(batches) < 2 || batches[0][0].EventKind() != session.EventToolApprovalDecided || batches[1][0].EventKind() != session.EventToolExecutionStarted {
					t.Error("执行早于批准和 started")
				}
				if !runtime.writeGrant.Load() || runtime.patchGrant.Load() {
					t.Error("Write grant 缺失或扩大到 Patch")
				}
			}
			results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation}, approveWrite(t, runtime, tool.ApprovalAllowSession))
			if (err != nil) != (failAt != 0) {
				t.Fatalf("durability 失败结果错误 %v", err)
			}
			wantCalls := 0
			if failAt == 0 || failAt == 3 {
				wantCalls = 1
			}
			if executor.calls != wantCalls {
				t.Fatal("执行次数错误", executor.calls)
			}
			if failAt == 1 && runtime.writeGrant.Load() {
				t.Fatal("失败的 allow 安装了 grant")
			}
			if failAt == 0 && (len(results) != 1 || results[0].Status() != tool.ResultSuccess) {
				t.Fatal("成功结果丢失")
			}
		})
	}
}

func TestWriteGrantIsIndependentAndHeadlessFailsClosed(t *testing.T) {
	invocation, c := runtimeWriteInvocation(t)
	executor := &fixedWriteExecutor{}
	runtime := writeTestRuntime(t, &fakeJournal{}, c, executor)
	runtime.patchGrant.Store(true)
	runtime.interactiveApprovals = false
	results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation})
	if err != nil || results[0].Code() != "approval_required" || executor.calls != 0 {
		t.Fatal("Patch grant 授权了 Write", err)
	}
	runtime.interactiveApprovals = true
	invocation, c = runtimeWriteInvocation(t)
	runtime.writePreparer = &fixedWritePreparer{candidate: c}
	if _, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation}, approveWrite(t, runtime, tool.ApprovalAllowSession)); err != nil {
		t.Fatal(err)
	}
	runtime.interactiveApprovals = false
	invocation, c = runtimeWriteInvocation(t)
	runtime.writePreparer = &fixedWritePreparer{candidate: c}
	if _, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation}, func(event protocol.Event) {
		if event.Kind == protocol.EventToolApprovalRequested {
			t.Fatal("已有 Write grant 仍发出请求")
		}
	}); err != nil {
		t.Fatal(err)
	}
	if executor.calls != 2 {
		t.Fatal("Write grant 未生效")
	}
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
			results, err := runtime.executeToolCalls(ctx, runtimeTurnID, []tool.Invocation{invocation})
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
	results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation, read})
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
				call := session.ReplayedToolCall{Invocation: invocation, State: state, ReadySequence: 2}
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
				if executor.calls != 0 || runtime.writePreparer.(*fixedWritePreparer).calls != 0 || runtime.writeGrant.Load() || len(runtime.readObservations.current) != 0 {
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

func TestWriteReadyDurabilityAndFinalizationFailBeforeCandidateReads(t *testing.T) {
	for _, boundary := range []string{"append", "finalize"} {
		t.Run(boundary, func(t *testing.T) {
			invocation, c := runtimeWriteInvocation(t)
			v, _ := invocation.Write()
			ready, _ := tool.NewWriteReadyCall(v.ProviderCallID(), v.Input())
			sample := newPreparedSampleWithReady(t, ready)
			journal := &fakeJournal{}
			if boundary == "append" {
				journal.failAt = 2
			} else {
				journal.hook = func(batch []session.RecordDraft) {
					if len(batch) == 3 && batch[2].EventKind() == session.EventToolCallReady {
						if err := sample.Finalize(); err != nil {
							t.Error(err)
						}
					}
				}
			}
			conversation := &fakeToolConversation{fakeConversation: &fakeConversation{stream: fixedStream(completedProviderEventWithSample(t, sample))}}
			runtime := newTestRuntime(t, conversation, journal)
			preparer := &fixedWritePreparer{candidate: c}
			executor := &fixedWriteExecutor{}
			runtime.writePreparer = preparer
			runtime.writeExecutor = executor
			if _, err := collectTurn(runtime, context.Background(), nil); err == nil {
				t.Fatal("ready 边界失败被忽略")
			}
			if preparer.calls != 0 || executor.calls != 0 {
				t.Fatal("ready durable/finalize 前读取候选")
			}
		})
	}
}

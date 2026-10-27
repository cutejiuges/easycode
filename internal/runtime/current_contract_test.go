package runtime

import (
	"context"
	"testing"

	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type denyCurrentPolicy struct{}

func (denyCurrentPolicy) Decide(tool.ReadyCall) tool.PolicyDecision { return tool.PolicyDeny }

func patchContractRuntime(t *testing.T, journal Journal) (*Runtime, tool.Invocation, *fixedPatchExecutor) {
	t.Helper()
	invocation, candidate := runtimePatchInvocation(t)
	executor := &fixedPatchExecutor{}
	return &Runtime{journal: journal, sessionID: runtimeSessionID, threadID: runtimeThreadID, patchPreparer: &fixedPatchPreparer{candidate: candidate}, patchExecutor: executor, toolPolicy: tool.NewApprovalPolicy(), newApprovalRequestID: tool.GenerateApprovalRequestID, interactiveApprovals: true}, invocation, executor
}

func TestPatchGrantRequiresDurableApproval(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{true: "sync-failed", false: "committed"}[fail], func(t *testing.T) {
			journal := &fakeJournal{}
			runtime, invocation, executor := patchContractRuntime(t, journal)
			if fail {
				journal.failAt = 1
			}
			journal.hook = func(drafts []session.RecordDraft) {
				if drafts[0].EventKind() == session.EventToolApprovalDecided && runtime.approvals.patchGrant.Load() {
					t.Error("grant installed before Sync")
				}
			}
			_, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation}, approveWrite(t, runtime, tool.ApprovalAllowSession))
			if (err != nil) != fail || runtime.approvals.patchGrant.Load() == fail || runtime.approvals.writeGrant.Load() {
				t.Fatalf("grant outcome: %v", err)
			}
			if fail && executor.calls != 0 {
				t.Fatal("execution followed failed approval")
			}
		})
	}
}

func TestFileGrantCannotOverridePolicyDeny(t *testing.T) {
	for _, kind := range []string{"Patch", "Write"} {
		t.Run(kind, func(t *testing.T) {
			journal := &fakeJournal{}
			runtime, invocation, patchExecutor := patchContractRuntime(t, journal)
			var writeExecutor *fixedWriteExecutor
			if kind == "Write" {
				var candidate tool.WriteCandidate
				invocation, candidate = runtimeWriteInvocation(t)
				writeExecutor = &fixedWriteExecutor{}
				runtime = writeTestRuntime(t, journal, candidate, writeExecutor)
			}
			runtime.approvals.patchGrant.Store(true)
			runtime.approvals.writeGrant.Store(true)
			runtime.toolPolicy = denyCurrentPolicy{}
			results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation})
			if err != nil || results[0].Status() != tool.ResultDenied || patchExecutor.calls != 0 || (writeExecutor != nil && writeExecutor.calls != 0) {
				t.Fatalf("deny overridden: %v", err)
			}
			draft := journal.snapshot()[0][0]
			record := session.Record{EventKind: draft.EventKind(), PayloadVersion: session.EnvelopeVersion, Payload: draft.PayloadBytes()}
			fact, err := session.DecodeToolApprovalDecidedPayload(record)
			if err != nil || fact.Source != session.ApprovalSourcePolicy {
				t.Fatalf("wrong source: %#v %v", fact, err)
			}
		})
	}
}

func TestPatchStartedCancellationLinearization(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		journal := &execAdmissionCancelJournal{cancel: cancel, accepted: accepted}
		runtime, invocation, executor := patchContractRuntime(t, journal)
		runtime.toolPolicy = allowPatchPolicy{}
		results, err := runtime.executeToolCalls(ctx, runtimeTurnID, []tool.Invocation{invocation})
		if err != nil || runtime.poisoned.Load() {
			t.Fatalf("healthy cancellation poisoned: %v", err)
		}
		if accepted && executor.calls != 1 {
			t.Fatal("accepted execution not resolved")
		}
		if !accepted && (executor.calls != 0 || results[0].Status() != tool.ResultCancelled) {
			t.Fatal("cancelled admission executed")
		}
	}
}

type mismatchedPatchExecutor struct {
	result tool.InvocationResult
	calls  int
}

func (executor *mismatchedPatchExecutor) Execute(context.Context, tool.PatchCandidate) tool.InvocationResult {
	executor.calls++
	return executor.result
}

func TestPatchInvalidResultPoisonsAndPairsRemainingCalls(t *testing.T) {
	journal := &fakeJournal{}
	runtime, invocation, _ := patchContractRuntime(t, journal)
	runtime.toolPolicy = allowPatchPolicy{}
	// 合法的其他 capability 结果不能冒充当前已执行调用。
	read := testRuntimeInvocations(t, 1)[0]
	wrong := tool.NewInvocationErrorResult(read, tool.ResultError, "bad", "bad")
	for _, result := range []tool.InvocationResult{wrong, tool.InvocationResult{}} {
		executor := &mismatchedPatchExecutor{result: result}
		runtime.patchExecutor = executor
		runtime.poisoned.Store(false)
		results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation, read}, func(protocol.Event) {})
		if err == nil || !runtime.poisoned.Load() || executor.calls != 1 || len(results) != 2 || results[0].Status() != tool.ResultOutcomeUncertain || results[0].Code() != "invalid_tool_result" || results[0].InvocationID() != invocation.InvocationID() || results[1].Status() != tool.ResultCancelled {
			t.Fatalf("invalid result did not close safely: %v %#v", err, results)
		}
	}
}

func TestWriteAndExecRejectMismatchedExecutionResults(t *testing.T) {
	for _, kind := range []string{"Write", "Exec"} {
		for _, variant := range []string{"zero", "other-call", "metadata"} {
			t.Run(kind+"/"+variant, func(t *testing.T) {
				journal := &fakeJournal{}
				var runtime *Runtime
				var invocation tool.Invocation
				var calls func() int
				var emit Emitter
				preview, _ := tool.NewModelPreview("completed")
				if kind == "Write" {
					var candidate tool.WriteCandidate
					invocation, candidate = runtimeWriteInvocation(t)
					executor := &fixedWriteExecutor{resultOverride: func(c tool.WriteCandidate) tool.InvocationResult {
						if variant == "zero" {
							return tool.InvocationResult{}
						}
						v := c.Invocation()
						if variant == "other-call" {
							call, _ := tool.NewWriteReadyCall("different-call", v.Input())
							other, _ := tool.NewInvocation(v.InvocationID(), call)
							w, _ := other.Write()
							return tool.NewWriteErrorResult(w, tool.ResultError, "failed", "failed")
						}
						metadata, _ := tool.NewWriteResultMetadata(v.Input().FilePath(), tool.WriteCreate, 4, 1, 0)
						result, err := tool.NewWriteInvocationResult(v, tool.ResultSuccess, "ok", preview, metadata)
						if err != nil {
							t.Fatal(err)
						}
						return result
					}}
					runtime = writeTestRuntime(t, journal, candidate, executor)
					runtime.toolPolicy = allowPatchPolicy{}
					calls = func() int { return executor.calls }
				} else {
					var executor *fixedExecComponents
					runtime, invocation, executor = runtimeExecFixture(t, journal)
					executor.resultOverride = func(c tool.ExecCandidate) tool.InvocationResult {
						if variant == "zero" {
							return tool.InvocationResult{}
						}
						v := c.Invocation()
						if variant == "other-call" {
							call, _ := tool.NewExecReadyCall("different-call", v.Input())
							other, _ := tool.NewInvocation(v.InvocationID(), call)
							e, _ := other.Exec()
							return tool.NewExecErrorResult(e, tool.ResultError, "failed", "failed")
						}
						metadata, _ := tool.NewExecResultMetadata("different-dir", 0, true, 0, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
						result, err := tool.NewExecInvocationResult(v, tool.ResultSuccess, "ok", preview, metadata)
						if err != nil {
							t.Fatal(err)
						}
						return result
					}
					calls = func() int { return executor.calls }
					events := []protocol.Event{}
					emit = execApprovalEmitter(t, runtime, tool.ApprovalAllowOnce, &events)
				}
				read := testRuntimeInvocations(t, 1)[0]
				results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation, read}, emit)
				if err == nil || calls() != 1 || !runtime.poisoned.Load() || len(results) != 2 || results[0].Status() != tool.ResultOutcomeUncertain || results[0].Code() != "invalid_tool_result" || results[0].ProviderCallID() != invocation.ProviderCallID() || results[1].Status() != tool.ResultCancelled {
					t.Fatalf("wrong result handling: %v %#v", err, results)
				}
			})
		}
	}
}

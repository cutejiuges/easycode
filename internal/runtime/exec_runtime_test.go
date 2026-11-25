package runtime

import (
	"context"
	"sync"
	"testing"

	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type fixedExecComponents struct {
	candidate      tool.ExecCandidate
	calls          int
	onExecute      func(context.Context)
	onProgress     func(func(tool.ExecProgress))
	resultOverride func(tool.ExecCandidate) tool.InvocationResult
}

func (components *fixedExecComponents) Prepare(tool.ExecInvocation) (tool.ExecCandidate, error) {
	return components.candidate, nil
}
func (components *fixedExecComponents) Execute(ctx context.Context, candidate tool.ExecCandidate, progress func(tool.ExecProgress)) tool.InvocationResult {
	components.calls++
	if components.onExecute != nil {
		components.onExecute(ctx)
	}
	if components.onProgress != nil {
		components.onProgress(progress)
	}
	if components.resultOverride != nil {
		return components.resultOverride(candidate)
	}
	metadata, _ := tool.NewExecResultMetadata(candidate.Invocation().Input().Workdir(), 0, true, 0, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
	preview, _ := tool.NewModelPreview("completed")
	result, _ := tool.NewExecInvocationResult(candidate.Invocation(), tool.ResultSuccess, "ok", preview, metadata)
	return result
}

func runtimeExecFixture(t *testing.T, journal Journal) (*Runtime, tool.Invocation, *fixedExecComponents) {
	t.Helper()
	input, _ := tool.NewExecInput("printf hello", "", 1000)
	call, _ := tool.NewExecReadyCall("call-exec", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewInvocation(id, call)
	exec, _ := invocation.Exec()
	shell, _ := tool.NewShellProfile("sh", "/bin/sh")
	environment, _ := tool.NewEnvironmentPolicy("minimal", []string{"PATH"})
	sandbox, _ := tool.NewSandboxProfile("workspace", false, true)
	candidate, err := tool.NewExecCandidate(exec, shell, environment, sandbox)
	requireNoError(t, err)
	components := &fixedExecComponents{candidate: candidate}
	runtime := &Runtime{journal: journal, sessionID: runtimeSessionID, threadID: runtimeThreadID, tools: tool.Components{PrepareExec: components, Exec: components}, newApprovalRequestID: tool.GenerateApprovalRequestID, interactiveApprovals: true}
	seedRuntimeToolCall(t, runtime, runtimeTurnID, invocation, 0, 0)
	return runtime, invocation, components
}

func execApprovalEmitter(t *testing.T, runtime *Runtime, decision tool.ApprovalDecision, events *[]protocol.Event) Emitter {
	t.Helper()
	return func(event protocol.Event) {
		*events = append(*events, event)
		if event.Kind != protocol.EventToolApprovalRequested {
			return
		}
		payload, err := protocol.DecodeToolApprovalRequested(event)
		requireNoError(t, err)
		requestID, _ := tool.ParseApprovalRequestID(payload.RequestID)
		invocationID, _ := tool.ParseInvocationID(payload.InvocationID)
		response, err := tool.NewApprovalResponse(requestID, runtimeTurnID, invocationID, tool.CandidateFingerprint(payload.Fingerprint), decision, "")
		requireNoError(t, err)
		for _, invalid := range []struct {
			request     tool.ApprovalRequestID
			fingerprint tool.CandidateFingerprint
			decision    tool.ApprovalDecision
		}{
			{requestID, tool.NewCandidateFingerprint([]byte("stale candidate")), decision},
			{tool.ApprovalRequestID("018f1d8a-7b5c-7def-8123-456789abcdee"), tool.CandidateFingerprint(payload.Fingerprint), decision},
			{requestID, tool.CandidateFingerprint(payload.Fingerprint), tool.ApprovalAllowSession},
		} {
			stale, err := tool.NewApprovalResponse(invalid.request, runtimeTurnID, invocationID, invalid.fingerprint, invalid.decision, "")
			requireNoError(t, err)
			if runtime.ResolveToolApproval(stale) == nil {
				t.Fatal("stale or session-wide Exec approval accepted")
			}
		}
		if err := runtime.ResolveToolApproval(response); err != nil {
			t.Fatal(err)
		}
		if err := runtime.ResolveToolApproval(response); err == nil {
			t.Fatal("duplicate Exec approval accepted")
		}
	}
}

type execAdmissionCancelJournal struct {
	fakeJournal
	cancel   context.CancelFunc
	accepted bool
}

func (journal *execAdmissionCancelJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	if len(drafts) == 1 && drafts[0].EventKind() == session.EventToolExecutionStarted {
		journal.cancel()
		if !journal.accepted {
			return nil, context.Canceled
		}
	}
	return journal.fakeJournal.AppendBatch(ctx, drafts)
}

func TestExecAdmissionCancellationLinearization(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		journal := &execAdmissionCancelJournal{cancel: cancel, accepted: accepted}
		runtime, invocation, executor := runtimeExecFixture(t, journal)
		var events []protocol.Event
		result, _, err := executeExecActivityTest(runtime, ctx, execApprovalEmitter(t, runtime, tool.ApprovalAllowOnce, &events), runtimeTurnID, invocation)
		cancel()
		if err != nil || runtime.poisoned.Load() {
			t.Fatalf("known cancellation poisoned execution: %v", err)
		}
		if accepted {
			if executor.calls != 1 {
				t.Fatal("accepted Exec was not invoked exactly once")
			}
		} else if executor.calls != 0 || result.Status() != tool.ResultCancelled || result.Code() != "cancelled_before_execution" {
			t.Fatal("unaccepted Exec created side effects")
		}
	}
}

func TestAcceptedExecApprovalSurvivesCancellationBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	journal := &fakeJournal{}
	runtime, invocation, executor := runtimeExecFixture(t, journal)
	var events []protocol.Event
	approve := execApprovalEmitter(t, runtime, tool.ApprovalAllowOnce, &events)
	result, _, err := executeExecActivityTest(runtime, ctx, func(event protocol.Event) {
		approve(event)
		if event.Kind == protocol.EventToolApprovalRequested {
			cancel()
		}
	}, runtimeTurnID, invocation)
	if err != nil || executor.calls != 0 || result.Code() != "cancelled_before_execution" {
		t.Fatalf("accepted approval and execution cancellation were conflated: %v, %v", result.Code(), err)
	}
	batches := journal.snapshot()
	if len(batches) != 2 || len(batches[0]) != 1 || len(batches[1]) != 1 || batches[1][0].EventKind() != session.EventToolCallResult {
		t.Fatal("expected one approval fact and one result without execution started")
	}
	draft := batches[0][0]
	fact, err := session.DecodeToolApprovalDecidedPayload(session.Record{EventKind: draft.EventKind(), Payload: draft.PayloadBytes()})
	if err != nil || fact.Outcome != tool.ApprovalOutcomeAllowOnce || fact.Source != session.ApprovalSourceInteractive {
		t.Fatalf("accepted interactive approval was rewritten: %v, %v", fact, err)
	}
}

func TestExecProgressCallbacksAreSerializedAndClosedAtReturn(t *testing.T) {
	_, _, executor := runtimeExecFixture(t, &fakeJournal{})
	var callback func(tool.ExecProgress)
	executor.onProgress = func(submit func(tool.ExecProgress)) {
		callback = submit
		var writers sync.WaitGroup
		for index := 0; index < 8; index++ {
			writers.Add(1)
			go func() {
				defer writers.Done()
				for update := 0; update < 100; update++ {
					progress, err := tool.NewExecProgress(1, 1, 0, 0, 0, "x", false)
					if err != nil {
						t.Error(err)
						return
					}
					submit(progress)
				}
			}()
		}
		writers.Wait()
	}
	count := 0
	result := executeWithProgress(context.Background(), executor, executor.candidate, func(progress tool.ExecProgress) {
		// 非原子计数也必须通过race测试，因为只有当前owner可以调用此投影。
		count++
		if progress.Recent() != "x" {
			t.Error("progress snapshot changed")
		}
	})
	if count == 0 || result.Status() != tool.ResultSuccess {
		t.Fatal("final progress was not delivered")
	}
	before := count
	late, _ := tool.NewExecProgress(2, 2, 0, 0, 0, "late", false)
	callback(late)
	if count != before {
		t.Fatal("late progress escaped after completion")
	}
}

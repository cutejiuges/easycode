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
	if err != nil {
		t.Fatal(err)
	}
	components := &fixedExecComponents{candidate: candidate}
	runtime := &Runtime{journal: journal, sessionID: runtimeSessionID, threadID: runtimeThreadID, execPreparer: components, execExecutor: components, newApprovalRequestID: tool.GenerateApprovalRequestID, interactiveApprovals: true}
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
		if err != nil {
			t.Fatal(err)
		}
		requestID, _ := tool.ParseApprovalRequestID(payload.RequestID)
		invocationID, _ := tool.ParseInvocationID(payload.InvocationID)
		response, err := tool.NewApprovalResponse(requestID, runtimeTurnID, invocationID, tool.CandidateFingerprint(payload.Fingerprint), decision, "")
		if err != nil {
			t.Fatal(err)
		}
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
			if err != nil {
				t.Fatal(err)
			}
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

func TestExecProgressIsTransientAndBoundToCurrentIdentity(t *testing.T) {
	journal := &fakeJournal{}
	runtime, invocation, executor := runtimeExecFixture(t, journal)
	executor.onProgress = func(submit func(tool.ExecProgress)) {
		progress, err := tool.NewExecProgress(0, 0, 0, 0, 0, "bounded recent output", false)
		if err != nil {
			t.Error(err)
			return
		}
		submit(progress)
	}
	var events []protocol.Event
	_, _, err := executeExecActivityTest(runtime, context.Background(), execApprovalEmitter(t, runtime, tool.ApprovalAllowOnce, &events), runtimeTurnID, invocation)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []protocol.EventKind
	for _, event := range events {
		if event.Kind != protocol.EventToolStarted && event.Kind != protocol.EventToolProgress && event.Kind != protocol.EventToolTerminal {
			continue
		}
		if err := event.Validate(); err != nil {
			t.Fatal(err)
		}
		payload, err := protocol.DecodeToolActivity(event)
		if err != nil || payload.InvocationID != string(invocation.InvocationID()) || event.SessionID != runtimeSessionID || event.ThreadID != runtimeThreadID || event.TurnID != runtimeTurnID {
			t.Fatal("command identity escaped current turn")
		}
		kinds = append(kinds, event.Kind)
	}
	if len(kinds) != 3 || kinds[0] != protocol.EventToolStarted || kinds[1] != protocol.EventToolProgress || kinds[2] != protocol.EventToolTerminal {
		t.Fatalf("command order = %v", kinds)
	}
	batches := journal.snapshot()
	if len(batches) != 3 {
		t.Fatalf("progress persisted: %d batches", len(batches))
	}
	for index, kind := range []session.EventKind{session.EventToolApprovalDecided, session.EventToolExecutionStarted, session.EventToolCallResult} {
		if len(batches[index]) != 1 || batches[index][0].EventKind() != kind {
			t.Fatal("progress changed durable vocabulary")
		}
	}
}

func TestExecApprovalAndAdmissionDurability(t *testing.T) {
	journal := &fakeJournal{}
	runtime, invocation, executor := runtimeExecFixture(t, journal)
	executor.onExecute = func(context.Context) {
		batches := journal.snapshot()
		if len(batches) != 2 || batches[0][0].EventKind() != session.EventToolApprovalDecided || batches[1][0].EventKind() != session.EventToolExecutionStarted {
			t.Error("Exec ran before durable approval and admission")
		}
	}
	var events []protocol.Event
	approvalEmitter := execApprovalEmitter(t, runtime, tool.ApprovalAllowOnce, &events)
	emit := func(event protocol.Event) {
		if event.Kind == protocol.EventToolTerminal && len(journal.snapshot()) != 3 {
			t.Fatal("terminal published before durable result")
		}
		approvalEmitter(event)
	}
	result, aborted, err := executeExecActivityTest(runtime, context.Background(), emit, runtimeTurnID, invocation)
	if err != nil || aborted || result.Status() != tool.ResultSuccess || executor.calls != 1 {
		t.Fatalf("Exec failed: %v", err)
	}
	batches := journal.snapshot()
	if len(batches) != 3 || batches[2][0].EventKind() != session.EventToolCallResult {
		t.Fatal("Exec result not persisted")
	}
	approval, err := session.DecodeToolApprovalDecidedPayload(session.Record{PayloadVersion: session.EnvelopeVersion, EventKind: session.EventToolApprovalDecided, Payload: batches[0][0].PayloadBytes()})
	if err != nil || approval.Source != session.ApprovalSourceInteractive || approval.Metadata.Capability != tool.CapabilityExec || approval.Metadata.TimeoutMillis != 1000 {
		t.Fatal("interactive approval source or metadata lost")
	}
	if len(events) != 4 || events[2].Kind != protocol.EventToolStarted || events[3].Kind != protocol.EventToolTerminal {
		t.Fatal("Exec activity order changed")
	}
	terminal, err := protocol.DecodeToolTerminal(events[3])
	if err != nil || terminal.ResultPreview != result.Preview().Text() {
		t.Fatal("terminal changed durable result bytes")
	}
}

func TestExecDurableFailuresNeverRepeatSideEffects(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		journal := &fakeJournal{failAt: failAt}
		runtime, invocation, executor := runtimeExecFixture(t, journal)
		var events []protocol.Event
		_, _, err := executeExecActivityTest(runtime, context.Background(), execApprovalEmitter(t, runtime, tool.ApprovalAllowOnce, &events), runtimeTurnID, invocation)
		want := 0
		if failAt == 3 {
			want = 1
		}
		if err == nil || !runtime.poisoned.Load() || executor.calls != want {
			t.Fatalf("failure %d crossed admission: %v calls=%d", failAt, err, executor.calls)
		}
	}
}

func TestExecHeadlessDenialHasNoExecution(t *testing.T) {
	journal := &fakeJournal{}
	runtime, invocation, executor := runtimeExecFixture(t, journal)
	runtime.interactiveApprovals = false
	var events []protocol.Event
	result, aborted, err := executeExecActivityTest(runtime, context.Background(), func(event protocol.Event) { events = append(events, event) }, runtimeTurnID, invocation)
	if err != nil || aborted || result.Code() != "approval_required" || result.Status() != tool.ResultDenied || executor.calls != 0 {
		t.Fatalf("headless execution admitted: %v", err)
	}
	for _, event := range events {
		if event.Kind == protocol.EventToolStarted || event.Kind == protocol.EventToolProgress {
			t.Fatal("denied command emitted execution activity")
		}
	}
	batches := journal.snapshot()
	if len(batches) != 1 || len(batches[0]) != 2 || batches[0][0].EventKind() != session.EventToolApprovalDecided || batches[0][1].EventKind() != session.EventToolCallResult {
		t.Fatal("headless denial ledger changed")
	}
	approval, err := session.DecodeToolApprovalDecidedPayload(session.Record{PayloadVersion: session.EnvelopeVersion, EventKind: session.EventToolApprovalDecided, Payload: batches[0][0].PayloadBytes()})
	if err != nil || approval.Source != session.ApprovalSourceHeadless {
		t.Fatal("headless denial source lost")
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

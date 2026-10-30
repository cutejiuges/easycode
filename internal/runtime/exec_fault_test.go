package runtime

import (
	"context"
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type execOutputFaultConversation struct {
	*fakeToolConversation
	prepared *provider.PreparedToolOutputs
}

func (conversation *execOutputFaultConversation) PrepareToolOutputs(results []tool.InvocationResult) (*provider.PreparedToolOutputs, error) {
	prepared, err := conversation.fakeToolConversation.PrepareToolOutputs(results)
	conversation.prepared = prepared
	return prepared, err
}

func TestExecTurnDurabilityAndFinalizersFailClosedAtEveryBoundary(t *testing.T) {
	for _, boundary := range []string{"append-1", "append-2", "append-3", "append-4", "append-5", "append-6", "append-7", "append-8", "sample-finalizer", "output-finalizer", "final-finalizer", "uncertain-result", "invalid-result"} {
		t.Run(boundary, func(t *testing.T) {
			journal := &fakeJournal{}
			for index := 1; index <= 8; index++ {
				if boundary == fmt.Sprintf("append-%d", index) {
					journal.failAt = index
				}
			}
			_, invocation, executor := runtimeExecFixture(t, journal)
			exec, _ := invocation.Exec()
			ready, _ := tool.NewExecReadyCall(exec.ProviderCallID(), exec.Input())
			first, final := newPreparedSampleWithReady(t, ready), newPreparedSample(t, func() {})
			base := &fakeConversation{}
			conversation := &execOutputFaultConversation{fakeToolConversation: &fakeToolConversation{fakeConversation: base}}
			base.stream = func(ctx context.Context, input provider.TurnInput) (<-chan provider.StreamEvent, error) {
				sample := first
				if base.calls.Load() == 2 {
					sample = final
				}
				return fixedStream(completedProviderEventWithSample(t, sample))(ctx, input)
			}
			journal.hook = func(drafts []session.RecordDraft) {
				var err error
				if boundary == "sample-finalizer" && len(drafts) == 3 && drafts[2].EventKind() == session.EventToolCallReady {
					err = first.Finalize()
				}
				if boundary == "output-finalizer" && len(drafts) == 1 && drafts[0].EventKind() == session.EventProviderNativeCommit {
					err = conversation.prepared.Finalize()
				}
				if boundary == "final-finalizer" && len(drafts) == 3 && drafts[2].EventKind() == session.EventTurnCompleted {
					err = final.Finalize()
				}
				if err != nil {
					t.Error(err)
				}
			}
			if boundary == "uncertain-result" {
				executor.resultOverride = func(candidate tool.ExecCandidate) tool.InvocationResult {
					return tool.NewExecErrorResult(candidate.Invocation(), tool.ResultOutcomeUncertain, "process_cleanup_uncertain", "Exec cleanup is uncertain")
				}
			}
			if boundary == "invalid-result" {
				executor.resultOverride = func(tool.ExecCandidate) tool.InvocationResult { return tool.InvocationResult{} }
			}
			config := testRuntimeConfig(t, journal)
			config.ExecPreparer, config.ExecExecutor, config.InteractiveApprovals = executor, executor, true
			config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
			config.GenerateInvocationID = func() (tool.InvocationID, error) { return invocation.InvocationID(), nil }
			runtime, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			var events []protocol.Event
			err = runtime.RunTurn(context.Background(), runtimeInputs("run command"), execApprovalEmitter(t, runtime, tool.ApprovalAllowOnce, &events))
			if err == nil || !runtime.poisoned.Load() {
				t.Fatalf("boundary did not poison: %v", err)
			}
			wantExecutions := 1
			if journal.failAt > 0 && journal.failAt <= 5 || boundary == "sample-finalizer" {
				wantExecutions = 0
			}
			if executor.calls != wantExecutions {
				t.Fatalf("executions = %d want %d", executor.calls, wantExecutions)
			}
			terminals := 0
			for _, event := range events {
				if event.Kind == protocol.EventTurnFailed || event.Kind == protocol.EventTurnCompleted {
					terminals++
				}
			}
			if terminals != 1 {
				t.Fatalf("turn terminals = %d", terminals)
			}
			providerCalls, prepareCalls := base.calls.Load(), conversation.prepareCalls.Load()
			if _, err := collectTurn(runtime, context.Background(), nil); err == nil {
				t.Fatal("poisoned session admitted another turn")
			}
			if executor.calls != wantExecutions || base.calls.Load() != providerCalls || conversation.prepareCalls.Load() != prepareCalls {
				t.Fatal("poisoned session repeated a side effect")
			}
			if journal.failAt > 0 && journal.failAt < 7 && final.Finalized() {
				t.Fatal("failed durable boundary finalized final sample")
			}
			if journal.failAt == 2 && first.Finalized() {
				t.Fatal("ready sample finalized without Sync")
			}
			if journal.failAt == 6 && conversation.finalized.Load() != 0 {
				t.Fatal("outputs finalized without Sync")
			}
		})
	}
}

func TestExecApprovalCancellationRejectsLateResponseAndNeverStarts(t *testing.T) {
	journal := &fakeJournal{}
	runtime, invocation, executor := runtimeExecFixture(t, journal)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var request protocol.ToolApprovalRequestedPayload
	var emitted []protocol.Event
	emit := func(event protocol.Event) {
		emitted = append(emitted, event)
		if event.Kind == protocol.EventToolApprovalRequested {
			var err error
			request, err = protocol.DecodeToolApprovalRequested(event)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
		}
	}
	_, _, err := executeExecActivityTest(runtime, ctx, emit, runtimeTurnID, invocation)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := tool.ParseApprovalRequestID(request.RequestID)
	response, _ := tool.NewApprovalResponse(id, runtimeTurnID, invocation.InvocationID(), tool.CandidateFingerprint(request.Fingerprint), tool.ApprovalAllowOnce, "")
	if runtime.ResolveToolApproval(response) == nil || executor.calls != 0 {
		t.Fatal("cancelled approval was reused")
	}
	for _, batch := range journal.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventToolExecutionStarted {
				t.Fatal("cancelled approval admitted execution")
			}
		}
	}
}

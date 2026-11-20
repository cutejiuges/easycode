package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

const (
	runtimeSessionID = domain.SessionID("00000000-0010-7000-8000-000000000010")
	runtimeThreadID  = domain.ThreadID("00000000-0011-7000-8000-000000000011")
	runtimeTurnID    = domain.TurnID("00000000-0012-7000-8000-000000000012")
)

type fakeConversation struct {
	stream    func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error)
	footprint func() (domain.NativeHistoryFootprint, error)
	history   domain.SemanticHistoryView
	calls     atomic.Int32
}

type runtimeReadExecutor struct{}

func (runtimeReadExecutor) Execute(context.Context, tool.ReadInvocation) tool.ReadExecution {
	return tool.ReadExecution{}
}

type runtimeGlobExecutor struct{}

func (runtimeGlobExecutor) Execute(context.Context, tool.GlobInvocation, tool.TextOutputCapture) tool.SearchExecution {
	return tool.SearchExecution{}
}

type runtimeGrepExecutor struct{}

func (runtimeGrepExecutor) Execute(context.Context, tool.GrepInvocation, tool.TextOutputCapture) tool.SearchExecution {
	return tool.SearchExecution{}
}

type recordingReadExecutor struct {
	mu           sync.Mutex
	calls        atomic.Int32
	cancelledCtx atomic.Bool
	invocations  []tool.ProviderCallID
	onExecute    func(tool.ReadInvocation)
}

type gatedReadExecutor struct {
	started   chan tool.ProviderCallID
	release   <-chan struct{}
	active    atomic.Int32
	peak      atomic.Int32
	calls     atomic.Int32
	completed sync.Mutex
	order     []tool.ProviderCallID
}

func (executor *gatedReadExecutor) Execute(_ context.Context, invocation tool.ReadInvocation) tool.ReadExecution {
	executor.calls.Add(1)
	active := executor.active.Add(1)
	for {
		peak := executor.peak.Load()
		if active <= peak || executor.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	executor.started <- invocation.ProviderCallID()
	<-executor.release
	executor.active.Add(-1)
	executor.completed.Lock()
	executor.order = append(executor.order, invocation.ProviderCallID())
	executor.completed.Unlock()
	result, _ := tool.NewReadExecution(tool.RenderReadSuccess(invocation, "README.md", []string{"content"}, 1), tool.UnavailableReadObservation())
	return result
}

type indexedGateReadExecutor struct {
	started   chan tool.ProviderCallID
	completed chan tool.ProviderCallID
	gates     map[tool.ProviderCallID]<-chan struct{}
	mu        sync.Mutex
	order     []tool.ProviderCallID
}

func (executor *indexedGateReadExecutor) Execute(_ context.Context, invocation tool.ReadInvocation) tool.ReadExecution {
	executor.started <- invocation.ProviderCallID()
	<-executor.gates[invocation.ProviderCallID()]
	executor.mu.Lock()
	executor.order = append(executor.order, invocation.ProviderCallID())
	executor.mu.Unlock()
	executor.completed <- invocation.ProviderCallID()
	result, _ := tool.NewReadExecution(tool.RenderReadSuccess(invocation, "README.md", []string{string(invocation.ProviderCallID())}, 1), tool.UnavailableReadObservation())
	return result
}

type startedFailureJournal struct {
	mu              sync.Mutex
	calls           int
	failAt          int
	poisoned        bool
	poisonOnFailure bool
	batches         [][]session.RecordDraft
	sequence        uint64
}

func (journal *startedFailureJournal) AppendBatch(_ context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if journal.poisoned {
		return nil, errors.New("fixture journal is poisoned")
	}
	journal.calls++
	if journal.calls == journal.failAt {
		journal.poisoned = journal.poisonOnFailure
		return nil, errors.New("fixture started append failure")
	}
	journal.batches = append(journal.batches, append([]session.RecordDraft(nil), drafts...))
	records := make([]session.Record, len(drafts))
	for i := range records {
		journal.sequence++
		records[i] = session.Record{Sequence: journal.sequence, TurnID: drafts[i].TurnID(), EventKind: drafts[i].EventKind(), Payload: drafts[i].PayloadBytes()}
	}
	return records, nil
}

func (journal *startedFailureJournal) Poisoned() bool {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return journal.poisoned
}

func (journal *startedFailureJournal) snapshot() [][]session.RecordDraft {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	result := make([][]session.RecordDraft, len(journal.batches))
	for index := range journal.batches {
		result[index] = append([]session.RecordDraft(nil), journal.batches[index]...)
	}
	return result
}

func (executor *recordingReadExecutor) Execute(ctx context.Context, invocation tool.ReadInvocation) tool.ReadExecution {
	executor.calls.Add(1)
	executor.mu.Lock()
	executor.invocations = append(executor.invocations, invocation.ProviderCallID())
	executor.mu.Unlock()
	if executor.onExecute != nil {
		executor.onExecute(invocation)
	}
	if ctx.Err() != nil {
		executor.cancelledCtx.Store(true)
		result, _ := tool.NewReadExecution(tool.NewReadErrorResult(invocation, tool.ResultCancelled, "cancelled", "Read cancelled", "."), tool.UnavailableReadObservation())
		return result
	}
	result, _ := tool.NewReadExecution(tool.RenderReadSuccess(invocation, "README.md", []string{"content"}, 1), tool.UnavailableReadObservation())
	return result
}

func (executor *recordingReadExecutor) invocationSnapshot() []tool.ProviderCallID {
	executor.mu.Lock()
	defer executor.mu.Unlock()
	return append([]tool.ProviderCallID(nil), executor.invocations...)
}

type fakeToolConversation struct {
	*fakeConversation
	prepareCalls atomic.Int32
	finalized    atomic.Int32
	discarded    atomic.Int32
	results      []tool.InvocationResult
	onFinalize   func()
}

func (conversation *fakeToolConversation) PrepareToolOutputs(results []tool.ModelOutput) (*provider.PreparedToolOutputs, error) {
	conversation.prepareCalls.Add(1)
	conversation.results = make([]tool.InvocationResult, len(results))
	for i, v := range results {
		conversation.results[i] = v.Result()
	}
	envelope, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, "responses", json.RawMessage(`{"shape":"tool_outputs"}`),
	)
	if err != nil {
		return nil, err
	}
	return provider.NewPreparedToolOutputs(envelope, func() {
		conversation.finalized.Add(1)
		if conversation.onFinalize != nil {
			conversation.onFinalize()
		}
	}, func() { conversation.discarded.Add(1) })
}

func (*fakeConversation) Family() domain.ProviderFamily { return domain.ProviderOpenAI }

func (*fakeConversation) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (conversation *fakeConversation) ProjectHistory() domain.SemanticHistoryView {
	if conversation.history.Provider.Valid() {
		turns := make([]domain.SemanticTurn, len(conversation.history.Turns))
		copy(turns, conversation.history.Turns)
		return domain.SemanticHistoryView{Provider: conversation.history.Provider, Turns: turns}
	}
	return domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: make([]domain.SemanticTurn, 0)}
}

func (conversation *fakeConversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	if conversation.footprint != nil {
		return conversation.footprint()
	}
	estimated, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, 0)
	return domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 0, estimated)
}

func (conversation *fakeConversation) Stream(ctx context.Context, input provider.TurnInput) (provider.StreamEvents, error) {
	conversation.calls.Add(1)
	events, err := conversation.stream(ctx, input)
	return testStreamEvents(events), err
}

func testStreamEvents(events <-chan provider.StreamEvent) provider.StreamEvents {
	if events == nil {
		return nil
	}
	return func(yield func(provider.StreamEvent) bool) {
		for event := range events {
			if !yield(event) {
				return
			}
		}
	}
}

type fakeJournal struct {
	mu       sync.Mutex
	batches  [][]session.RecordDraft
	sequence uint64
	calls    int
	failAt   int
	poisoned bool
	hook     func([]session.RecordDraft)
}

func (journal *fakeJournal) AppendBatch(_ context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	journal.mu.Lock()
	journal.calls++
	call := journal.calls
	cloned := append([]session.RecordDraft(nil), drafts...)
	hook := journal.hook
	if journal.failAt == call {
		journal.poisoned = true
		journal.mu.Unlock()
		return nil, errors.New("fixture journal failure")
	}
	journal.batches = append(journal.batches, cloned)
	records := make([]session.Record, len(drafts))
	for i := range records {
		journal.sequence++
		records[i].Sequence = journal.sequence
	}
	journal.mu.Unlock()
	if hook != nil {
		hook(cloned)
	}
	return records, nil
}

func (journal *fakeJournal) Poisoned() bool {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return journal.poisoned
}

func (journal *fakeJournal) snapshot() [][]session.RecordDraft {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	result := make([][]session.RecordDraft, len(journal.batches))
	for index, batch := range journal.batches {
		result[index] = append([]session.RecordDraft(nil), batch...)
	}
	return result
}

func TestRunTurnDurableSuccessOrderingAndIdentity(t *testing.T) {
	t.Parallel()
	var timelineMu sync.Mutex
	var timeline []string
	addTimeline := func(value string) {
		timelineMu.Lock()
		timeline = append(timeline, value)
		timelineMu.Unlock()
	}
	delta, err := protocol.NewAssistantTextDelta("hello")
	if err != nil {
		t.Fatal(err)
	}
	prepared := newPreparedSample(t, func() { addTimeline("finalize") })
	conversation := &fakeConversation{stream: func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		addTimeline("provider")
		return fixedStream(
			semanticProviderEvent(t, delta),
			completedProviderEventWithSample(t, prepared),
		)(context.Background(), provider.TurnInput{})
	}}
	journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
		addTimeline("append:" + string(drafts[0].EventKind()))
	}}
	runtime := newTestRuntime(t, conversation, journal)
	events, runErr := collectTurn(runtime, context.Background(), func(event protocol.Event) {
		addTimeline("emit:" + string(event.Kind))
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventAssistantTextDelta, protocol.EventTurnCompleted)
	for _, event := range events {
		if event.SessionID != runtimeSessionID || event.ThreadID != runtimeThreadID || event.TurnID != runtimeTurnID {
			t.Fatalf("event identity = %#v", event)
		}
	}
	wantTimeline := []string{
		"append:turn_started", "emit:turn_started", "append:turn_inputs_committed", "emit:user_input_committed", "emit:transcript_committed", "provider", "emit:assistant_text_delta",
		"append:provider_native_commit", "finalize", "emit:transcript_committed", "emit:turn_completed",
	}
	if fmt.Sprint(timeline) != fmt.Sprint(wantTimeline) {
		t.Fatalf("timeline = %#v, want %#v", timeline, wantTimeline)
	}
	batches := journal.snapshot()
	if len(batches) != 3 || len(batches[0]) != 1 || len(batches[2]) != 3 ||
		batches[2][0].EventKind() != session.EventProviderNativeCommit ||
		batches[2][1].EventKind() != session.EventSampleUsage ||
		batches[2][2].EventKind() != session.EventTurnCompleted {
		t.Fatalf("journal batches = %#v", batches)
	}
	commit, err := session.DecodeNativeCommitPayload(session.Record{
		EventKind: batches[2][0].EventKind(), Payload: batches[2][0].PayloadBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if commit.Provider != domain.ProviderOpenAI || commit.Wire != "responses" || string(commit.Payload) != `{"shape":"text_sample"}` {
		t.Fatalf("native commit = %#v", commit)
	}
	usageRecord, err := session.DecodeSampleUsagePayload(session.Record{
		EventKind: batches[2][1].EventKind(), Payload: batches[2][1].PayloadBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantUsage := testRuntimeUsage(t)
	gotUsage, err := usageRecord.Domain()
	if err != nil || gotUsage != wantUsage {
		t.Fatalf("sample usage = %#v, %v", gotUsage, err)
	}
	completed, err := protocol.DecodeTurnCompleted(events[len(events)-1])
	if err != nil {
		t.Fatal(err)
	}
	completedUsage, err := completed.Usage.Domain()
	if err != nil || completedUsage != wantUsage {
		t.Fatalf("completed usage = %#v, %v", completedUsage, err)
	}
}

func TestRunTurnCancellationUsesStartedAcceptanceAsExecutorBoundary(t *testing.T) {
	for _, test := range []struct {
		name              string
		cancelOnStarted   bool
		wantExecutorCalls int32
		wantStarted       bool
	}{
		{name: "before started acceptance", wantExecutorCalls: 0},
		{name: "after started acceptance", cancelOnStarted: true, wantExecutorCalls: 1, wantStarted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			executor := &recordingReadExecutor{}
			ready := testRuntimeReadyCall(t, "call-cancel")
			callSample := newPreparedSampleWithReady(t, ready)
			conversation := &fakeToolConversation{fakeConversation: &fakeConversation{
				stream: fixedStream(completedProviderEventWithSample(t, callSample)),
			}}
			journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
				if test.cancelOnStarted {
					if len(drafts) == 1 && drafts[0].EventKind() == session.EventToolExecutionStarted {
						cancel()
					}
					return
				}
				if len(drafts) == 3 && drafts[2].EventKind() == session.EventToolCallReady {
					cancel()
				}
			}}
			config := testRuntimeConfig(t, journal)
			config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Read = executor })
			config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
			config.GenerateInvocationID = func() (tool.InvocationID, error) {
				return tool.ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789ab")
			}
			runtime, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			events, runErr := collectTurn(runtime, ctx, nil)
			if !errors.Is(runErr, context.Canceled) {
				t.Fatalf("RunTurn() error = %v", runErr)
			}
			assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventTurnFailed)
			if executor.calls.Load() != test.wantExecutorCalls {
				t.Fatalf("executor calls = %d", executor.calls.Load())
			}
			if test.wantStarted && !executor.cancelledCtx.Load() {
				t.Fatal("executor did not receive the cancelled context")
			}
			batches := journal.snapshot()
			hasStarted := false
			for _, batch := range batches {
				if len(batch) == 1 && batch[0].EventKind() == session.EventToolExecutionStarted {
					hasStarted = true
				}
			}
			if hasStarted != test.wantStarted || conversation.prepareCalls.Load() != 1 ||
				conversation.finalized.Load() != 1 || len(conversation.results) != 1 ||
				conversation.results[0].Status() != tool.ResultCancelled {
				t.Fatalf(
					"started=%t prepare/finalize/results=%d/%d/%#v",
					hasStarted, conversation.prepareCalls.Load(), conversation.finalized.Load(), conversation.results,
				)
			}
		})
	}
}

func TestParallelToolSchedulerBoundsConcurrency(t *testing.T) {
	for _, callCount := range []int{1, 8, 9} {
		t.Run(fmt.Sprintf("calls_%d", callCount), func(t *testing.T) {
			release := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			executor := &gatedReadExecutor{started: make(chan tool.ProviderCallID, callCount), release: release}
			runtime := newRuntimeWithReadExecutor(t, &fakeJournal{}, executor)
			invocations := testRuntimeInvocations(t, callCount)
			type outcome struct {
				results []tool.InvocationResult
				err     error
			}
			done := make(chan outcome, 1)
			go func() {
				results, err := executeReadyFixtureCalls(t, runtime, context.Background(), runtimeTurnID, invocations)
				done <- outcome{results: results, err: err}
			}()
			initial := callCount
			if initial > maxParallelReadWorkers {
				initial = maxParallelReadWorkers
			}
			for range initial {
				<-executor.started
			}
			if callCount > maxParallelReadWorkers {
				select {
				case unexpected := <-executor.started:
					t.Fatalf("worker 上限释放前启动了额外调用 %s", unexpected)
				default:
				}
			}
			releaseOnce.Do(func() { close(release) })
			result := <-done
			if result.err != nil || len(result.results) != callCount {
				t.Fatalf("executeToolCalls() results/error = %d/%v", len(result.results), result.err)
			}
			if executor.calls.Load() != int32(callCount) || executor.peak.Load() != int32(initial) || executor.active.Load() != 0 {
				t.Fatalf("calls/peak/active = %d/%d/%d", executor.calls.Load(), executor.peak.Load(), executor.active.Load())
			}
		})
	}
}

func TestParallelToolSchedulerPersistsCallOrderAfterOutOfOrderCompletion(t *testing.T) {
	gates := map[tool.ProviderCallID]chan struct{}{
		"call-0": make(chan struct{}),
		"call-1": make(chan struct{}),
		"call-2": make(chan struct{}),
	}
	executor := &indexedGateReadExecutor{
		started: make(chan tool.ProviderCallID, 3), completed: make(chan tool.ProviderCallID, 3),
		gates: map[tool.ProviderCallID]<-chan struct{}{
			"call-0": gates["call-0"], "call-1": gates["call-1"], "call-2": gates["call-2"],
		},
	}
	journal := &fakeJournal{}
	runtime := newRuntimeWithReadExecutor(t, journal, executor)
	invocations := testRuntimeInvocations(t, 3)
	type outcome struct {
		results []tool.InvocationResult
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := executeReadyFixtureCalls(t, runtime, context.Background(), runtimeTurnID, invocations)
		done <- outcome{results: results, err: err}
	}()
	for range 3 {
		<-executor.started
	}
	for _, callID := range []tool.ProviderCallID{"call-2", "call-0", "call-1"} {
		close(gates[callID])
		if completed := <-executor.completed; completed != callID {
			t.Fatalf("completion = %s, want %s", completed, callID)
		}
	}
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	for index, invocationResult := range result.results {
		want := tool.ProviderCallID(fmt.Sprintf("call-%d", index))
		if invocationResult.ProviderCallID() != want {
			t.Fatalf("result[%d] call ID = %s", index, invocationResult.ProviderCallID())
		}
	}
	batches := journal.snapshot()
	if len(batches) != 6 {
		t.Fatalf("scheduler batches = %d", len(batches))
	}
	for index := range 3 {
		if batches[index][0].EventKind() != session.EventToolExecutionStarted ||
			batches[index+3][0].EventKind() != session.EventToolCallResult {
			t.Fatalf("scheduler batch order = %#v", batches)
		}
		persisted := decodeRuntimeResultDraft(t, batches[index+3][0], invocations[index])
		if persisted.ProviderCallID() != tool.ProviderCallID(fmt.Sprintf("call-%d", index)) {
			t.Fatalf("persisted result[%d] = %s", index, persisted.ProviderCallID())
		}
	}
}

func TestStartedAppendFailureWaitsAcceptedCallsAndStopsLaterCalls(t *testing.T) {
	for _, test := range []struct {
		name     string
		poisoned bool
	}{
		{name: "definite"},
		{name: "uncertain", poisoned: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := []tool.ReadyCall{
				testRuntimeReadyCall(t, "call-0"), testRuntimeReadyCall(t, "call-1"), testRuntimeReadyCall(t, "call-2"),
			}
			sample := newPreparedSampleWithReady(t, calls...)
			executor := &recordingReadExecutor{}
			conversation := &fakeToolConversation{fakeConversation: &fakeConversation{
				stream: fixedStream(completedProviderEventWithSample(t, sample)),
			}}
			journal := &startedFailureJournal{failAt: 5, poisonOnFailure: test.poisoned}
			config := testRuntimeConfig(t, journal)
			config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Read = executor })
			config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
			invocationIDs := testRuntimeInvocationIDs(t, 3)
			var invocationIndex int
			config.GenerateInvocationID = func() (tool.InvocationID, error) {
				value := invocationIDs[invocationIndex]
				invocationIndex++
				return value, nil
			}
			runtime, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			events, runErr := collectTurn(runtime, context.Background(), nil)
			if !errors.Is(runErr, &fault.Error{Code: fault.CodeSessionWrite}) {
				t.Fatalf("RunTurn() error = %v", runErr)
			}
			assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventTurnFailed)
			if executor.calls.Load() != 1 || fmt.Sprint(executor.invocationSnapshot()) != fmt.Sprint([]tool.ProviderCallID{"call-0"}) ||
				conversation.calls.Load() != 1 || conversation.prepareCalls.Load() != 0 {
				t.Fatalf("executor calls/order, streams, prepare = %d/%#v/%d/%d", executor.calls.Load(), executor.invocationSnapshot(), conversation.calls.Load(), conversation.prepareCalls.Load())
			}
			batches := journal.snapshot()
			resultCount := 0
			failureCount := 0
			for _, batch := range batches {
				if batch[0].EventKind() == session.EventToolCallResult {
					resultCount++
				}
				if batch[0].EventKind() == session.EventTurnFailed {
					failureCount++
				}
			}
			if failureCount != 0 || !runtime.poisoned.Load() {
				t.Fatalf("unfinished call group lost its recovery boundary: failure=%d, runtime poisoned=%t", failureCount, runtime.poisoned.Load())
			}
			wantResults := 1
			if test.poisoned {
				wantResults = 0
			}
			if resultCount != wantResults || journal.Poisoned() != test.poisoned {
				t.Fatalf("durable accepted results=%d, journal poisoned=%t", resultCount, journal.Poisoned())
			}
		})
	}
}

func TestRunTurnToolDurabilityFailuresPreventLaterSideEffects(t *testing.T) {
	t.Run("call sample append", func(t *testing.T) {
		ready := testRuntimeReadyCall(t, "call-failed-ready")
		envelope, err := provider.NewNativeCommitEnvelope(
			domain.ProviderOpenAI, "responses", json.RawMessage(`{"shape":"call_sample"}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		var finalized atomic.Int32
		var discarded atomic.Int32
		sample, err := provider.NewPreparedSample(
			envelope, testRuntimeUsage(t), syntheticRequestReceipt(domain.RequestSample), domain.EmptySamplePresentation(1), func() { finalized.Add(1) }, func() { discarded.Add(1) }, ready,
		)
		if err != nil {
			t.Fatal(err)
		}
		executor := &recordingReadExecutor{}
		conversation := &fakeToolConversation{fakeConversation: &fakeConversation{
			stream: fixedStream(completedProviderEventWithSample(t, sample)),
		}}
		journal := &fakeJournal{failAt: 3}
		config := testRuntimeConfig(t, journal)
		config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Read = executor })
		config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
		runtime, err := New(conversation, config)
		if err != nil {
			t.Fatal(err)
		}
		events, runErr := collectTurn(runtime, context.Background(), nil)
		if !errors.Is(runErr, &fault.Error{Code: fault.CodeSessionWrite}) {
			t.Fatalf("RunTurn() error = %v", runErr)
		}
		assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventTurnFailed)
		if finalized.Load() != 0 || discarded.Load() != 1 || executor.calls.Load() != 0 ||
			conversation.prepareCalls.Load() != 0 {
			t.Fatalf(
				"finalize/discard/executor/output calls = %d/%d/%d/%d",
				finalized.Load(), discarded.Load(), executor.calls.Load(), conversation.prepareCalls.Load(),
			)
		}
	})

	t.Run("call sample finalizer", func(t *testing.T) {
		sample := newPreparedSampleWithReady(t, testRuntimeReadyCall(t, "call-failed-finalizer"))
		executor := &recordingReadExecutor{}
		conversation := &fakeToolConversation{fakeConversation: &fakeConversation{
			stream: fixedStream(completedProviderEventWithSample(t, sample)),
		}}
		journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
			if len(drafts) == 3 && drafts[2].EventKind() == session.EventToolCallReady {
				if err := sample.Finalize(); err != nil {
					t.Errorf("fixture finalize: %v", err)
				}
			}
		}}
		config := testRuntimeConfig(t, journal)
		config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Read = executor })
		config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
		runtime, err := New(conversation, config)
		if err != nil {
			t.Fatal(err)
		}
		events, runErr := collectTurn(runtime, context.Background(), nil)
		if !errors.Is(runErr, &fault.Error{Code: fault.CodeStreamProtocol}) {
			t.Fatalf("RunTurn() error = %v", runErr)
		}
		assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventTurnFailed)
		if executor.calls.Load() != 0 || conversation.prepareCalls.Load() != 0 || !sample.Finalized() {
			t.Fatalf(
				"executor/output/finalized = %d/%d/%t",
				executor.calls.Load(), conversation.prepareCalls.Load(), sample.Finalized(),
			)
		}
	})

	t.Run("tool output append", func(t *testing.T) {
		executor := &recordingReadExecutor{}
		conversation := &fakeToolConversation{fakeConversation: &fakeConversation{
			stream: fixedStream(completedProviderEventWithSample(
				t, newPreparedSampleWithReady(t, testRuntimeReadyCall(t, "call-failed-output")),
			)),
		}}
		journal := &fakeJournal{failAt: 6}
		config := testRuntimeConfig(t, journal)
		config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Read = executor })
		config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
		runtime, err := New(conversation, config)
		if err != nil {
			t.Fatal(err)
		}
		events, runErr := collectTurn(runtime, context.Background(), nil)
		if !errors.Is(runErr, &fault.Error{Code: fault.CodeSessionWrite}) {
			t.Fatalf("RunTurn() error = %v", runErr)
		}
		assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventTurnFailed)
		if executor.calls.Load() != 1 || conversation.prepareCalls.Load() != 1 ||
			conversation.finalized.Load() != 0 || conversation.discarded.Load() != 1 ||
			conversation.calls.Load() != 1 {
			t.Fatalf(
				"executor/prepare/finalize/discard/stream calls = %d/%d/%d/%d/%d",
				executor.calls.Load(), conversation.prepareCalls.Load(), conversation.finalized.Load(),
				conversation.discarded.Load(), conversation.calls.Load(),
			)
		}
	})
}

func TestReconcileToolTurnClosesCrashPointsWithoutExternalCalls(t *testing.T) {
	invocation := testRuntimeInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ab", "call-recovery")
	readInvocation, _ := invocation.Read()
	durableResult := tool.RenderReadSuccess(readInvocation, "README.md", []string{"persisted"}, 1)
	for _, test := range []struct {
		name             string
		calls            []session.ReplayedToolCall
		outputsCommitted bool
		wantBatches      int
		wantResultStatus tool.ResultStatus
		wantResultCode   string
		wantFailureCode  string
		wantPrepareCalls int32
	}{
		{
			name: "ready", calls: []session.ReplayedToolCall{{
				ReadySequence: 4, SampleIndex: 0, CallIndex: 0,
				Invocation: invocation, State: session.ReplayedToolCallReady,
			}},
			wantBatches: 3, wantResultStatus: tool.ResultCancelled,
			wantResultCode:  "session_interrupted_before_execution",
			wantFailureCode: "session_interrupted", wantPrepareCalls: 1,
		},
		{
			name: "started", calls: []session.ReplayedToolCall{{
				ReadySequence: 4, StartedSequence: 5, SampleIndex: 0, CallIndex: 0,
				Invocation: invocation, State: session.ReplayedToolCallStarted,
			}},
			wantBatches: 3, wantResultStatus: tool.ResultOutcomeUncertain,
			wantResultCode: "outcome_uncertain", wantFailureCode: "outcome_uncertain", wantPrepareCalls: 1,
		},
		{
			name: "result", calls: []session.ReplayedToolCall{{
				ReadySequence: 4, StartedSequence: 5, ResultSequence: 6, SampleIndex: 0, CallIndex: 0,
				Invocation: invocation, State: session.ReplayedToolCallResult, Result: durableResult,
			}},
			wantBatches: 2, wantFailureCode: "session_interrupted", wantPrepareCalls: 1,
		},
		{
			name: "outputs committed", outputsCommitted: true,
			wantBatches: 1, wantFailureCode: "session_interrupted",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := range test.calls {
				test.calls[i].Origin = runtimeFixtureOrigin(t, test.calls[i].SampleIndex, test.calls[i].CallIndex)
			}
			plan, err := session.NewToolRecoveryPlan(runtimeTurnID, 1, test.calls, test.outputsCommitted)
			if err != nil {
				t.Fatal(err)
			}
			executor := &recordingReadExecutor{}
			conversation := &fakeToolConversation{fakeConversation: &fakeConversation{
				stream: func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
					return nil, errors.New("unexpected Provider stream")
				},
			}}
			journal := &fakeJournal{}
			config := testRuntimeConfig(t, journal)
			config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Read = executor })
			runtime, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.ReconcileToolTurn(context.Background(), plan); err != nil {
				t.Fatal(err)
			}
			if executor.calls.Load() != 0 || conversation.calls.Load() != 0 ||
				conversation.prepareCalls.Load() != test.wantPrepareCalls {
				t.Fatalf(
					"executor/stream/prepare calls = %d/%d/%d",
					executor.calls.Load(), conversation.calls.Load(), conversation.prepareCalls.Load(),
				)
			}
			batches := journal.snapshot()
			if len(batches) != test.wantBatches || len(batches[len(batches)-1]) != 1 ||
				batches[len(batches)-1][0].EventKind() != session.EventTurnFailed {
				t.Fatalf("reconciliation batches = %#v", batches)
			}
			failure := decodeRuntimeFailureDraft(t, batches[len(batches)-1][0])
			if failure.Code != test.wantFailureCode {
				t.Fatalf("failure = %#v", failure)
			}
			if test.wantResultStatus != "" {
				result := decodeRuntimeResultDraft(t, batches[0][0], invocation)
				if result.Status() != test.wantResultStatus || result.Code() != test.wantResultCode {
					t.Fatalf("result status/code = %s/%s", result.Status(), result.Code())
				}
			}
			if test.wantPrepareCalls == 1 && conversation.finalized.Load() != 1 {
				t.Fatalf("tool output finalizer calls = %d", conversation.finalized.Load())
			}
		})
	}
}

func TestRunTurnRejectsEventAfterTerminalWithoutFinalizing(t *testing.T) {
	t.Parallel()
	var finalized atomic.Int32
	prepared := newPreparedSample(t, func() { finalized.Add(1) })
	delta, _ := protocol.NewAssistantTextDelta("late")
	journal := &fakeJournal{}
	runtime := newTestRuntime(t, &fakeConversation{stream: fixedStream(
		completedProviderEventWithSample(t, prepared),
		semanticProviderEvent(t, delta),
	)}, journal)
	events, err := collectTurn(runtime, context.Background(), nil)
	if !errors.Is(err, &fault.Error{Code: fault.CodeStreamProtocol}) {
		t.Fatalf("RunTurn() error = %v", err)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventTurnFailed)
	if finalized.Load() != 0 || len(journal.snapshot()) != 3 {
		t.Fatalf("finalized/batches = %d/%#v", finalized.Load(), journal.snapshot())
	}
}

func TestRunTurnCancelsAndDrainsAfterInvalidEvent(t *testing.T) {
	t.Parallel()
	cancelObserved := make(chan struct{})
	producerDone := make(chan struct{})
	delta, err := protocol.NewAssistantTextDelta("drained")
	if err != nil {
		t.Fatal(err)
	}
	tail := semanticProviderEvent(t, delta)
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent)
		go func() {
			defer close(producerDone)
			defer close(stream)
			stream <- provider.StreamEvent{}
			<-ctx.Done()
			close(cancelObserved)
			stream <- tail
		}()
		return stream, nil
	}}
	journal := &fakeJournal{}
	runtime := newTestRuntime(t, conversation, journal)
	events, runErr := collectTurn(runtime, context.Background(), nil)
	if !errors.Is(runErr, &fault.Error{Code: fault.CodeStreamProtocol}) {
		t.Fatalf("RunTurn() error = %v", runErr)
	}
	<-cancelObserved
	<-producerDone
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventUserInputCommitted, protocol.EventTurnFailed)
	batches := journal.snapshot()
	if len(batches) != 3 || len(batches[2]) != 1 || batches[2][0].EventKind() != session.EventTurnFailed {
		t.Fatalf("durable batches = %#v", batches)
	}
}

func TestRunTurnCancelCompletionRacePublishesOneTerminal(t *testing.T) {
	for iteration := 0; iteration < 32; iteration++ {
		ctx, cancel := context.WithCancel(context.Background())
		prepared := newPreparedSample(t, func() {})
		conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
			stream := make(chan provider.StreamEvent, 1)
			go func() {
				select {
				case <-ctx.Done():
					stream <- cancelledProviderEvent(t, context.Canceled)
				default:
					stream <- completedProviderEventWithSample(t, prepared)
				}
				close(stream)
			}()
			return stream, nil
		}}
		runtime := newTestRuntime(t, conversation, &fakeJournal{})
		var terminalCount atomic.Int32
		var wait sync.WaitGroup
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = runtime.runFixtureTurn(ctx, runtimeInputs("hello"), func(event protocol.Event) {
				if event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed {
					terminalCount.Add(1)
				}
			})
		}()
		cancel()
		wait.Wait()
		if terminalCount.Load() != 1 {
			t.Fatalf("iteration %d terminal count = %d", iteration, terminalCount.Load())
		}
	}
}

func newTestRuntime(t *testing.T, conversation provider.Conversation, journal Journal) *Runtime {
	t.Helper()
	config := testRuntimeConfig(t, journal)
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func testRuntimeConfig(t *testing.T, journal Journal) Config {
	t.Helper()
	profile, err := contextplan.NewProviderProfile(domain.ProviderOpenAI, "gpt-test")
	if err != nil {
		t.Fatal(err)
	}
	projectInstructions, err := domain.NewEmptyProjectInstructionsSnapshot(32 << 10)
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		SessionID: runtimeSessionID, ThreadID: runtimeThreadID, Journal: journal,
		ProviderWire:        domain.ProviderOpenAI.Wire(),
		InitialTaskProgress: domain.NewNoTaskProgress(),
		ContextProfile:      profile, ToolBinding: testRuntimeToolBinding(t, nil), ProjectInstructions: projectInstructions,
		ContextBudget:  contextplan.DisabledBudget(),
		ContextPlanner: contextplan.NewPlanner(),
	}
}

func testRuntimeToolBinding(t *testing.T, alter func(*tool.Components)) tool.Binding {
	t.Helper()
	components := tool.Components{
		Read: runtimeReadExecutor{}, Glob: runtimeGlobExecutor{}, Grep: runtimeGrepExecutor{},
		PreparePatch: catalogPatchPreparer{}, Patch: catalogPatchExecutor{},
		PrepareWrite: catalogWritePreparer{}, Write: catalogWriteExecutor{},
		PrepareExec: catalogExecFixture{}, Exec: catalogExecFixture{}, Policy: tool.NewApprovalPolicy(),
		ReadArtifact: catalogArtifactExecutor{}, Skill: catalogSkillExecutor{},
		Question: catalogQuestionFixture{}, Task: catalogTaskFixture{},
	}
	if alter != nil {
		alter(&components)
	}
	binding, err := tool.NewBinding(components)
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func newRuntimeWithReadExecutor(t *testing.T, journal Journal, executor tool.ReadExecutor) *Runtime {
	t.Helper()
	conversation := &fakeConversation{
		stream: func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
			return nil, errors.New("unexpected Provider stream")
		},
	}
	config := testRuntimeConfig(t, journal)
	config.ToolBinding = testRuntimeToolBinding(t, func(tools *tool.Components) { tools.Read = executor })
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func testRuntimeInvocationIDs(t *testing.T, count int) []tool.InvocationID {
	t.Helper()
	result := make([]tool.InvocationID, count)
	for index := range count {
		value, err := tool.ParseInvocationID(fmt.Sprintf("01890f3e-7bcd-7abc-8abc-%012x", index+1))
		if err != nil {
			t.Fatal(err)
		}
		result[index] = value
	}
	return result
}

func testRuntimeInvocations(t *testing.T, count int) []tool.Invocation {
	t.Helper()
	identities := testRuntimeInvocationIDs(t, count)
	result := make([]tool.Invocation, count)
	for index := range count {
		invocation, err := tool.NewInvocation(identities[index], testRuntimeReadyCall(t, fmt.Sprintf("call-%d", index)))
		if err != nil {
			t.Fatal(err)
		}
		result[index] = invocation
	}
	return result
}

func testRuntimeReadyCall(t *testing.T, callValue string) tool.ReadyCall {
	t.Helper()
	callID, err := tool.ParseProviderCallID(callValue)
	if err != nil {
		t.Fatal(err)
	}
	input, err := tool.NewReadInput("README.md", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	call, err := tool.NewReadReadyCall(callID, input)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func testRuntimeInvocation(t *testing.T, invocationValue string, callValue string) tool.Invocation {
	t.Helper()
	invocationID, err := tool.ParseInvocationID(invocationValue)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewInvocation(invocationID, testRuntimeReadyCall(t, callValue))
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func newPreparedSample(t *testing.T, finalize func()) *provider.PreparedSample {
	t.Helper()
	envelope, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, "responses", json.RawMessage(`{"shape":"text_sample"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.NewPreparedSample(envelope, testRuntimeUsage(t), syntheticRequestReceipt(domain.RequestSample), domain.EmptySamplePresentation(0), finalize, func() {})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func newPreparedSampleWithReady(t *testing.T, ready ...tool.ReadyCall) *provider.PreparedSample {
	t.Helper()
	envelope, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, "responses", json.RawMessage(`{"shape":"call_sample"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.NewPreparedSample(envelope, testRuntimeUsage(t), syntheticRequestReceipt(domain.RequestSample), domain.EmptySamplePresentation(uint32(len(ready))), func() {}, func() {}, ready...)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func decodeRuntimeResultDraft(
	t *testing.T,
	draft session.RecordDraft,
	invocation tool.Invocation,
) tool.InvocationResult {
	t.Helper()
	payload, err := session.DecodeToolCallResultPayload(session.Record{
		EventKind: draft.EventKind(), Payload: draft.PayloadBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := payload.Domain(invocation)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func decodeRuntimeFailureDraft(t *testing.T, draft session.RecordDraft) session.TurnFailedPayload {
	t.Helper()
	payload, err := session.DecodeTurnFailedPayload(session.Record{
		EventKind: draft.EventKind(), Payload: draft.PayloadBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func testRuntimeUsage(t *testing.T) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(8),
		domain.KnownUsageMetric(2),
		domain.UnknownUsageMetric(),
		domain.KnownUsageMetric(5),
		domain.NotApplicableUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

func fixedStream(events ...provider.StreamEvent) func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
	return func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, len(events))
		for _, event := range events {
			stream <- event
		}
		close(stream)
		return stream, nil
	}
}

func semanticProviderEvent(t *testing.T, event protocol.Event) provider.StreamEvent {
	t.Helper()
	streamEvent, err := provider.NewSemanticStreamEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	return streamEvent
}

func completedProviderEventWithSample(t *testing.T, sample *provider.PreparedSample) provider.StreamEvent {
	t.Helper()
	event, err := provider.NewCompletedStreamEvent(sample)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func cancelledProviderEvent(t *testing.T, failure error) provider.StreamEvent {
	t.Helper()
	event, err := provider.NewCancelledStreamEvent(failure)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func collectTurn(runtime *Runtime, ctx context.Context, observe Emitter) ([]protocol.Event, error) {
	var events []protocol.Event
	err := runtime.runFixtureTurn(ctx, runtimeInputs("hello"), func(event protocol.Event) {
		events = append(events, event)
		if observe != nil {
			observe(event)
		}
	})
	return events, err
}

func assertEventKinds(t *testing.T, events []protocol.Event, want ...protocol.EventKind) {
	t.Helper()
	events = validateToolActivitiesAndConversation(t, events)
	if len(events) != len(want) {
		t.Fatalf("event count: got %d want %d (%#v)", len(events), len(want), eventKindsForTest(events))
	}
	for index := range want {
		if events[index].Kind != want[index] {
			t.Fatalf("event %d: got %s want %s", index, events[index].Kind, want[index])
		}
	}
}

func TestChatSessionShutdownWaitsForDurableFailure(t *testing.T) {
	t.Parallel()
	blocked := make(chan struct{})
	release := make(chan struct{})
	var blockOnce sync.Once
	journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
		if drafts[0].EventKind() != session.EventTurnFailed {
			return
		}
		blockOnce.Do(func() { close(blocked) })
		<-release
	}}
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		go func() {
			<-ctx.Done()
			stream <- cancelledProviderEvent(t, context.Canceled)
			close(stream)
		}()
		return stream, nil
	}}
	sessionFacade := NewChatSession(newTestRuntime(t, conversation, journal))
	if err := sessionFacade.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sessionFacade.Shutdown(context.Background()) })
	events, err := sessionFacade.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Kind != protocol.EventTurnStarted {
		t.Fatalf("first event = %s", event.Kind)
	}
	sessionFacade.Interrupt()
	<-blocked
	waitContext, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if err := sessionFacade.Shutdown(waitContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Shutdown() error = %v", err)
	}
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- sessionFacade.Shutdown(context.Background()) }()
	close(release)
	if err := <-shutdownDone; err != nil {
		t.Fatal(err)
	}
	for range events {
	}
}

// catalogPatchPreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogPatchPreparer struct{}

func (catalogPatchPreparer) Prepare(context.Context, tool.PatchInvocation) (tool.PatchCandidate, error) {
	return tool.PatchCandidate{}, nil
}

type catalogPatchExecutor struct{}

func (catalogPatchExecutor) Execute(context.Context, tool.PatchCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

// catalogWritePreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogWritePreparer struct{}

func (catalogWritePreparer) Prepare(context.Context, tool.WriteInvocation, tool.ReadObservation) (tool.WriteCandidate, error) {
	return tool.WriteCandidate{}, nil
}

type catalogWriteExecutor struct{}

func (catalogWriteExecutor) Execute(context.Context, tool.WriteCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

// fakeToolOutputPreflight 为内存测试执行与生产相同的身份校验。
type fakeToolOutputPreflight struct{ invocation tool.Invocation }

func (p fakeToolOutputPreflight) Validate(output tool.ModelOutput) error {
	r := output.Result()
	if r.Validate() != nil || r.InvocationID() != p.invocation.InvocationID() || r.ProviderCallID() != p.invocation.ProviderCallID() || r.Capability() != p.invocation.Capability() {
		return fmt.Errorf("invalid fake output")
	}
	return nil
}
func (c *fakeToolConversation) PrepareToolOutputPreflight(inv tool.Invocation) (provider.ToolOutputPreflight, error) {
	return fakeToolOutputPreflight{inv}, inv.Validate()
}

// ProjectAssistantSample 在不模拟原生采样的测试替身中明确失败，不猜测展示列表。
func (*fakeConversation) ProjectAssistantSample(domain.AssistantSampleReference) (domain.AssistantSampleText, error) {
	return domain.AssistantSampleText{}, domain.HookConfigInvalid
}

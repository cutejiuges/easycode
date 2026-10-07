package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func (runtimeReadExecutor) Execute(context.Context, tool.ReadInvocation) tool.InvocationResult {
	return tool.InvocationResult{}
}

type recordingReadExecutor struct {
	calls        atomic.Int32
	cancelledCtx atomic.Bool
	invocations  []tool.ProviderCallID
	onExecute    func(tool.ReadInvocation)
}

func (executor *recordingReadExecutor) Execute(ctx context.Context, invocation tool.ReadInvocation) tool.InvocationResult {
	executor.calls.Add(1)
	executor.invocations = append(executor.invocations, invocation.ProviderCallID())
	if executor.onExecute != nil {
		executor.onExecute(invocation)
	}
	if ctx.Err() != nil {
		executor.cancelledCtx.Store(true)
		return tool.NewReadErrorResult(invocation, tool.ResultCancelled, "cancelled", "Read cancelled", ".")
	}
	return tool.RenderReadSuccess(invocation, "README.md", []string{"content"}, 1)
}

type fakeToolConversation struct {
	*fakeConversation
	prepareCalls atomic.Int32
	finalized    atomic.Int32
	discarded    atomic.Int32
	results      []tool.InvocationResult
	onFinalize   func()
}

func (conversation *fakeToolConversation) PrepareToolOutputs(results []tool.InvocationResult) (*provider.PreparedToolOutputs, error) {
	conversation.prepareCalls.Add(1)
	conversation.results = append([]tool.InvocationResult(nil), results...)
	envelope, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, "responses", 1, json.RawMessage(`{"shape":"tool_outputs"}`),
	)
	if err != nil {
		return nil, err
	}
	return provider.NewPreparedToolOutputsWithDiscard(envelope, func() {
		conversation.finalized.Add(1)
		if conversation.onFinalize != nil {
			conversation.onFinalize()
		}
	}, func() { conversation.discarded.Add(1) })
}

type contextPlannerFunc func(contextplan.PlanningInput) (contextplan.ContextPlan, error)

func (plan contextPlannerFunc) Plan(input contextplan.PlanningInput) (contextplan.ContextPlan, error) {
	return plan(input)
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

func (conversation *fakeConversation) Stream(ctx context.Context, input provider.TurnInput) (<-chan provider.StreamEvent, error) {
	conversation.calls.Add(1)
	return conversation.stream(ctx, input)
}

type fakeJournal struct {
	mu       sync.Mutex
	batches  [][]session.RecordDraft
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
	journal.mu.Unlock()
	if hook != nil {
		hook(cloned)
	}
	return make([]session.Record, len(drafts)), nil
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
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventAssistantTextDelta, protocol.EventTurnCompleted)
	for _, event := range events {
		if event.SessionID != runtimeSessionID || event.ThreadID != runtimeThreadID || event.TurnID != runtimeTurnID {
			t.Fatalf("event identity = %#v", event)
		}
	}
	wantTimeline := []string{
		"append:turn_started", "emit:turn_started", "provider", "emit:assistant_text_delta",
		"append:provider_native_commit", "finalize", "emit:turn_completed",
	}
	if fmt.Sprint(timeline) != fmt.Sprint(wantTimeline) {
		t.Fatalf("timeline = %#v, want %#v", timeline, wantTimeline)
	}
	batches := journal.snapshot()
	if len(batches) != 2 || len(batches[0]) != 1 || len(batches[1]) != 3 ||
		batches[1][0].EventKind() != session.EventProviderNativeCommit ||
		batches[1][1].EventKind() != session.EventSampleUsage ||
		batches[1][2].EventKind() != session.EventTurnCompleted {
		t.Fatalf("journal batches = %#v", batches)
	}
	commit, err := session.DecodeNativeCommitPayload(session.Record{
		PayloadVersion: 1, ReplayRequirement: session.ReplayRequired,
		EventKind: batches[1][0].EventKind(), Payload: batches[1][0].PayloadBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if commit.Provider != domain.ProviderOpenAI || commit.Wire != "responses" || string(commit.Payload) != `{"shape":"text_sample"}` {
		t.Fatalf("native commit = %#v", commit)
	}
	usageRecord, err := session.DecodeSampleUsagePayload(session.Record{
		PayloadVersion: 1, ReplayRequirement: session.ReplayRequired,
		EventKind: batches[1][1].EventKind(), Payload: batches[1][1].PayloadBytes(),
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
			config.ReadExecutor = executor
			config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
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
			assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
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

func TestRunTurnCompletesOrderedMultiSampleToolLoop(t *testing.T) {
	var timeline []string
	appendTimeline := func(value string) { timeline = append(timeline, value) }
	executor := &recordingReadExecutor{onExecute: func(invocation tool.ReadInvocation) {
		appendTimeline("execute:" + string(invocation.ProviderCallID()))
	}}
	first := newPreparedSampleWithReady(
		t, testRuntimeReadyCall(t, "call-1"), testRuntimeReadyCall(t, "call-2"),
	)
	second := newPreparedSample(t, func() { appendTimeline("final_sample_finalize") })
	base := &fakeConversation{}
	conversation := &fakeToolConversation{fakeConversation: base, onFinalize: func() {
		appendTimeline("tool_outputs_finalize")
	}}
	base.stream = func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		switch base.calls.Load() {
		case 1:
			appendTimeline("first_stream")
			return fixedStream(completedProviderEventWithSample(t, first))(context.Background(), provider.TurnInput{})
		case 2:
			appendTimeline("second_stream")
			return fixedStream(completedProviderEventWithSample(t, second))(context.Background(), provider.TurnInput{})
		default:
			return nil, errors.New("unexpected Provider stream")
		}
	}
	journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
		if len(drafts) == 1 && drafts[0].EventKind() == session.EventProviderNativeCommit {
			appendTimeline("tool_outputs_sync")
		}
	}}
	config := testRuntimeConfig(t, journal)
	config.ReadExecutor = executor
	config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	invocationIDs := []string{
		"01890f3e-7bcd-7abc-8abc-0123456789ab",
		"01890f3e-7bcd-7abc-8abc-0123456789ac",
	}
	var invocationIndex int
	config.GenerateInvocationID = func() (tool.InvocationID, error) {
		value := invocationIDs[invocationIndex]
		invocationIndex++
		return tool.ParseInvocationID(value)
	}
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	events, err := collectTurn(runtime, context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnCompleted)
	if base.calls.Load() != 2 || executor.calls.Load() != 2 ||
		fmt.Sprint(executor.invocations) != fmt.Sprint([]tool.ProviderCallID{"call-1", "call-2"}) ||
		len(conversation.results) != 2 {
		t.Fatalf(
			"stream/executor/order/results = %d/%d/%#v/%d",
			base.calls.Load(), executor.calls.Load(), executor.invocations, len(conversation.results),
		)
	}
	batches := journal.snapshot()
	wantKinds := [][]session.EventKind{
		{session.EventTurnStarted},
		{session.EventProviderNativeCommit, session.EventSampleUsage, session.EventToolCallReady, session.EventToolCallReady},
		{session.EventToolExecutionStarted},
		{session.EventToolCallResult},
		{session.EventToolExecutionStarted},
		{session.EventToolCallResult},
		{session.EventProviderNativeCommit},
		{session.EventProviderNativeCommit, session.EventSampleUsage, session.EventTurnCompleted},
	}
	if len(batches) != len(wantKinds) {
		t.Fatalf("batch count = %d", len(batches))
	}
	for batchIndex, want := range wantKinds {
		if len(batches[batchIndex]) != len(want) {
			t.Fatalf("batch %d = %#v", batchIndex, batches[batchIndex])
		}
		for recordIndex, kind := range want {
			if batches[batchIndex][recordIndex].EventKind() != kind {
				t.Fatalf("batch %d record %d = %s", batchIndex, recordIndex, batches[batchIndex][recordIndex].EventKind())
			}
		}
	}
	wantTimeline := []string{
		"first_stream", "execute:call-1", "execute:call-2",
		"tool_outputs_sync", "tool_outputs_finalize", "second_stream", "final_sample_finalize",
	}
	if fmt.Sprint(timeline) != fmt.Sprint(wantTimeline) {
		t.Fatalf("timeline = %#v, want %#v", timeline, wantTimeline)
	}
	completed, err := protocol.DecodeTurnCompleted(events[len(events)-1])
	if err != nil {
		t.Fatal(err)
	}
	usage, err := completed.Usage.Domain()
	if err != nil {
		t.Fatal(err)
	}
	wantUsage, err := domain.AggregateSampleUsage([]domain.SampleUsage{testRuntimeUsage(t), testRuntimeUsage(t)})
	if err != nil || usage != wantUsage {
		t.Fatalf("turn usage = %#v, want %#v, error=%v", usage, wantUsage, err)
	}
}

func TestRunTurnToolDurabilityFailuresPreventLaterSideEffects(t *testing.T) {
	t.Run("call sample append", func(t *testing.T) {
		ready := testRuntimeReadyCall(t, "call-failed-ready")
		envelope, err := provider.NewNativeCommitEnvelope(
			domain.ProviderOpenAI, "responses", 1, json.RawMessage(`{"shape":"call_sample"}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		var finalized atomic.Int32
		var discarded atomic.Int32
		sample, err := provider.NewPreparedSampleWithDiscard(
			envelope, testRuntimeUsage(t), func() { finalized.Add(1) }, func() { discarded.Add(1) }, ready,
		)
		if err != nil {
			t.Fatal(err)
		}
		executor := &recordingReadExecutor{}
		conversation := &fakeToolConversation{fakeConversation: &fakeConversation{
			stream: fixedStream(completedProviderEventWithSample(t, sample)),
		}}
		journal := &fakeJournal{failAt: 2}
		config := testRuntimeConfig(t, journal)
		config.ReadExecutor = executor
		config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
		config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
		runtime, err := New(conversation, config)
		if err != nil {
			t.Fatal(err)
		}
		events, runErr := collectTurn(runtime, context.Background(), nil)
		if !errors.Is(runErr, &fault.Error{Code: fault.CodeSessionWrite}) {
			t.Fatalf("RunTurn() error = %v", runErr)
		}
		assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
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
		config.ReadExecutor = executor
		config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
		config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
		runtime, err := New(conversation, config)
		if err != nil {
			t.Fatal(err)
		}
		events, runErr := collectTurn(runtime, context.Background(), nil)
		if !errors.Is(runErr, &fault.Error{Code: fault.CodeStreamProtocol}) {
			t.Fatalf("RunTurn() error = %v", runErr)
		}
		assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
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
		journal := &fakeJournal{failAt: 5}
		config := testRuntimeConfig(t, journal)
		config.ReadExecutor = executor
		config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
		config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
		runtime, err := New(conversation, config)
		if err != nil {
			t.Fatal(err)
		}
		events, runErr := collectTurn(runtime, context.Background(), nil)
		if !errors.Is(runErr, &fault.Error{Code: fault.CodeSessionWrite}) {
			t.Fatalf("RunTurn() error = %v", runErr)
		}
		assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
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

func TestRunTurnConvertsInvalidExecutorResultAndContinuesSampling(t *testing.T) {
	first := newPreparedSampleWithReady(t, testRuntimeReadyCall(t, "call-invalid-result"))
	second := newPreparedSample(t, func() {})
	base := &fakeConversation{}
	conversation := &fakeToolConversation{fakeConversation: base}
	base.stream = func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		if base.calls.Load() == 1 {
			return fixedStream(completedProviderEventWithSample(t, first))(context.Background(), provider.TurnInput{})
		}
		return fixedStream(completedProviderEventWithSample(t, second))(context.Background(), provider.TurnInput{})
	}
	runtime := newTestRuntime(t, conversation, &fakeJournal{})
	events, err := collectTurn(runtime, context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnCompleted)
	if len(conversation.results) != 1 || conversation.results[0].Status() != tool.ResultError ||
		conversation.results[0].Code() != "invalid_tool_result" || base.calls.Load() != 2 {
		t.Fatalf("tool results/streams = %#v/%d", conversation.results, base.calls.Load())
	}
}

func TestRunTurnToolCallLimitAllowsFinalSampleAndRejectsNextCall(t *testing.T) {
	for _, test := range []struct {
		name           string
		callCount      int
		wantSuccess    bool
		wantExecutions int32
		wantStreams    int32
	}{
		{name: "sixty four calls then final text", callCount: maxToolCallsPerTurn, wantSuccess: true, wantExecutions: maxToolCallsPerTurn, wantStreams: 2},
		{name: "sixty fifth call rejected", callCount: maxToolCallsPerTurn + 1, wantExecutions: 0, wantStreams: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ready := make([]tool.ReadyCall, test.callCount)
			for index := range ready {
				ready[index] = testRuntimeReadyCall(t, fmt.Sprintf("call-%d", index))
			}
			callSample := newPreparedSampleWithReady(t, ready...)
			finalSample := newPreparedSample(t, func() {})
			base := &fakeConversation{}
			conversation := &fakeToolConversation{fakeConversation: base}
			base.stream = func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
				if base.calls.Load() == 1 {
					return fixedStream(completedProviderEventWithSample(t, callSample))(context.Background(), provider.TurnInput{})
				}
				return fixedStream(completedProviderEventWithSample(t, finalSample))(context.Background(), provider.TurnInput{})
			}
			executor := &recordingReadExecutor{}
			config := testRuntimeConfig(t, &fakeJournal{})
			config.ReadExecutor = executor
			config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
			config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
			runtime, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			events, runErr := collectTurn(runtime, context.Background(), nil)
			if test.wantSuccess {
				if runErr != nil {
					t.Fatal(runErr)
				}
				assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnCompleted)
			} else {
				if runErr == nil || !strings.Contains(runErr.Error(), "tool_loop_limit_exceeded") {
					t.Fatalf("RunTurn() error = %v", runErr)
				}
				assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
			}
			if executor.calls.Load() != test.wantExecutions || base.calls.Load() != test.wantStreams {
				t.Fatalf("executor/stream calls = %d/%d", executor.calls.Load(), base.calls.Load())
			}
		})
	}
}

func TestRunTurnSampleLimitStopsBeforeAdditionalProviderOrToolSideEffects(t *testing.T) {
	executor := &recordingReadExecutor{}
	base := &fakeConversation{}
	conversation := &fakeToolConversation{fakeConversation: base}
	base.stream = func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		callIndex := base.calls.Load()
		ready := testRuntimeReadyCall(t, fmt.Sprintf("sample-call-%d", callIndex))
		sample := newPreparedSampleWithReady(t, ready)
		return fixedStream(completedProviderEventWithSample(t, sample))(context.Background(), provider.TurnInput{})
	}
	config := testRuntimeConfig(t, &fakeJournal{})
	config.ReadExecutor = executor
	config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	events, runErr := collectTurn(runtime, context.Background(), nil)
	if runErr == nil || !strings.Contains(runErr.Error(), "tool_loop_limit_exceeded") {
		t.Fatalf("RunTurn() error = %v", runErr)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
	if base.calls.Load() != maxSamplesPerTurn || executor.calls.Load() != maxSamplesPerTurn ||
		conversation.prepareCalls.Load() != maxSamplesPerTurn {
		t.Fatalf(
			"stream/executor/output calls = %d/%d/%d",
			base.calls.Load(), executor.calls.Load(), conversation.prepareCalls.Load(),
		)
	}
}

func TestReconcileToolTurnClosesCrashPointsWithoutExternalCalls(t *testing.T) {
	invocation := testRuntimeInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ab", "call-recovery")
	durableResult := tool.RenderReadSuccess(invocation, "README.md", []string{"persisted"}, 1)
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
			config.ReadExecutor = executor
			config.ToolCatalog = mustRuntimeToolCatalogWithExecutor(t, executor)
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

func TestRunTurnStartWriteFailureMakesZeroProviderCalls(t *testing.T) {
	t.Parallel()
	conversation := &fakeConversation{stream: fixedStream()}
	journal := &fakeJournal{failAt: 1}
	runtime := newTestRuntime(t, conversation, journal)
	events, err := collectTurn(runtime, context.Background(), nil)
	if !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
		t.Fatalf("RunTurn() error = %v", err)
	}
	if conversation.calls.Load() != 0 || len(events) != 1 || events[0].Kind != protocol.EventTurnFailed {
		t.Fatalf("calls/events = %d/%#v", conversation.calls.Load(), events)
	}
	if _, err := collectTurn(runtime, context.Background(), nil); !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
		t.Fatalf("second RunTurn() error = %v", err)
	}
	if journal.calls != 1 {
		t.Fatalf("journal calls = %d", journal.calls)
	}
}

func TestRunTurnStopsConfirmedContextOverageBeforeProviderStream(t *testing.T) {
	t.Parallel()
	var timelineMu sync.Mutex
	var timeline []string
	add := func(value string) {
		timelineMu.Lock()
		timeline = append(timeline, value)
		timelineMu.Unlock()
	}
	conversation := &fakeConversation{stream: func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		add("provider:stream")
		return fixedStream()(context.Background(), provider.TurnInput{})
	}}
	journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
		add("append:" + string(drafts[0].EventKind()))
	}}
	config := testRuntimeConfig(t, journal)
	budget, err := contextplan.NewBudget(1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	config.ContextBudget = budget
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	events, runErr := collectTurn(runtime, context.Background(), func(event protocol.Event) {
		add("emit:" + string(event.Kind))
	})
	if !errors.Is(runErr, &fault.Error{Code: fault.CodeContextLimitExceeded}) {
		t.Fatalf("RunTurn() error = %v", runErr)
	}
	if strings.Contains(runErr.Error(), "hello") {
		t.Fatalf("over-limit error leaked input: %v", runErr)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
	if conversation.calls.Load() != 0 {
		t.Fatalf("provider calls = %d", conversation.calls.Load())
	}
	want := []string{"append:turn_started", "emit:turn_started", "append:turn_failed", "emit:turn_failed"}
	if fmt.Sprint(timeline) != fmt.Sprint(want) {
		t.Fatalf("timeline = %#v, want %#v", timeline, want)
	}
	batches := journal.snapshot()
	if len(batches) != 2 || batches[0][0].EventKind() != session.EventTurnStarted || batches[1][0].EventKind() != session.EventTurnFailed {
		t.Fatalf("journal batches = %#v", batches)
	}
}

func TestRunTurnPlanningFailureDoesNotStartProvider(t *testing.T) {
	t.Parallel()
	conversation := &fakeConversation{stream: fixedStream()}
	journal := &fakeJournal{}
	config := testRuntimeConfig(t, journal)
	config.ContextPlanner = contextPlannerFunc(func(contextplan.PlanningInput) (contextplan.ContextPlan, error) {
		return contextplan.ContextPlan{}, errors.New("fixture planning failure")
	})
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	events, runErr := collectTurn(runtime, context.Background(), nil)
	if !errors.Is(runErr, &fault.Error{Code: fault.CodeTurnFailed}) || conversation.calls.Load() != 0 {
		t.Fatalf("planning failure = %v provider calls=%d", runErr, conversation.calls.Load())
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
}

func TestRunTurnUsesRuntimeProjectInstructionsForPlanAndProvider(t *testing.T) {
	t.Parallel()
	const projectMarker = "runtime-project-only-marker"
	startupSnapshot := testRuntimeProjectInstructions(t, "AGENTS.md", projectMarker)
	callerSnapshot := testRuntimeProjectInstructions(t, "AGENTS.md", "caller-supplied-marker")
	var planned contextplan.ContextPlan
	var providerSnapshot domain.ProjectInstructionsSnapshot
	conversation := &fakeConversation{stream: func(_ context.Context, input provider.TurnInput) (<-chan provider.StreamEvent, error) {
		var exists bool
		var err error
		providerSnapshot, exists, err = input.ProjectInstructions()
		if err != nil || !exists {
			t.Fatalf("provider project instructions = exists %t, err %v", exists, err)
		}
		return fixedStream(completedProviderEventWithSample(t, newPreparedSample(t, func() {})))(context.Background(), input)
	}}
	journal := &fakeJournal{}
	config := testRuntimeConfig(t, journal)
	config.ProjectInstructions = startupSnapshot
	config.ContextPlanner = contextPlannerFunc(func(input contextplan.PlanningInput) (contextplan.ContextPlan, error) {
		var err error
		planned, err = contextplan.NewPlanner().Plan(input)
		return planned, err
	})
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	runtimeValue, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	callerInput, err := (provider.TurnInput{Text: "hello"}).WithProjectInstructions(callerSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	var events []protocol.Event
	if err := runtimeValue.RunTurn(context.Background(), callerInput, func(event protocol.Event) {
		events = append(events, event)
	}); err != nil {
		t.Fatal(err)
	}
	if providerSnapshot.Revision() != startupSnapshot.Revision() || providerSnapshot.Revision() == callerSnapshot.Revision() {
		t.Fatalf("provider snapshot revision = %q", providerSnapshot.Revision())
	}
	sources := planned.Sources()
	if len(sources) != 5 || sources[2].Kind() != contextplan.SourceProjectInstructions || sources[2].Revision() != startupSnapshot.Revision() {
		t.Fatalf("planned sources = %#v", sources)
	}
	for _, event := range events {
		encoded, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if strings.Contains(string(encoded), projectMarker) {
			t.Fatalf("runtime event contains project instructions: %s", encoded)
		}
	}
	for _, batch := range journal.snapshot() {
		for _, draft := range batch {
			if strings.Contains(string(draft.PayloadBytes()), projectMarker) {
				t.Fatalf("journal draft contains project instructions: %s", draft.PayloadBytes())
			}
		}
	}
}

func TestRunTurnContinuesForNonBlockingBudgetStates(t *testing.T) {
	t.Parallel()
	budget, err := contextplan.NewBudget(1000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	unknownEstimate, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristic)
	unknownFootprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 0, unknownEstimate)
	tests := []struct {
		name      string
		budget    contextplan.Budget
		footprint func() (domain.NativeHistoryFootprint, error)
	}{
		{name: "not enforced", budget: contextplan.DisabledBudget()},
		{name: "within limit", budget: budget},
		{name: "indeterminate", budget: budget, footprint: func() (domain.NativeHistoryFootprint, error) { return unknownFootprint, nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepared := newPreparedSample(t, func() {})
			conversation := &fakeConversation{
				stream:    fixedStream(completedProviderEventWithSample(t, prepared)),
				footprint: test.footprint,
			}
			config := testRuntimeConfig(t, &fakeJournal{})
			config.ContextBudget = test.budget
			config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
			runtime, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			events, runErr := collectTurn(runtime, context.Background(), nil)
			if runErr != nil || conversation.calls.Load() != 1 {
				t.Fatalf("RunTurn() error=%v calls=%d", runErr, conversation.calls.Load())
			}
			assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnCompleted)
		})
	}
}

func TestRunTurnCompletionWriteFailureDoesNotFinalizeAndPoisons(t *testing.T) {
	t.Parallel()
	var finalized atomic.Int32
	prepared := newPreparedSample(t, func() { finalized.Add(1) })
	conversation := &fakeConversation{stream: fixedStream(completedProviderEventWithSample(t, prepared))}
	journal := &fakeJournal{failAt: 2}
	runtime := newTestRuntime(t, conversation, journal)
	events, err := collectTurn(runtime, context.Background(), nil)
	if !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
		t.Fatalf("RunTurn() error = %v", err)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
	if finalized.Load() != 0 || prepared.Finalized() {
		t.Fatalf("prepared sample was finalized: %d", finalized.Load())
	}
	if journal.calls != 2 {
		t.Fatalf("journal calls = %d, failure path retried poisoned writer", journal.calls)
	}
	if _, err := collectTurn(runtime, context.Background(), nil); !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
		t.Fatalf("next RunTurn() error = %v", err)
	}
}

func TestRunTurnFinalizerConflictAfterDurableSuccessPoisonsWithoutPublishingUsage(t *testing.T) {
	t.Parallel()
	var finalized atomic.Int32
	prepared := newPreparedSample(t, func() { finalized.Add(1) })
	journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
		if len(drafts) == 3 && drafts[0].EventKind() == session.EventProviderNativeCommit {
			if err := prepared.Finalize(); err != nil {
				t.Errorf("fixture finalize: %v", err)
			}
		}
	}}
	runtime := newTestRuntime(t, &fakeConversation{stream: fixedStream(completedProviderEventWithSample(t, prepared))}, journal)
	events, err := collectTurn(runtime, context.Background(), nil)
	if !errors.Is(err, &fault.Error{Code: fault.CodeStreamProtocol}) {
		t.Fatalf("RunTurn() error = %v", err)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
	if finalized.Load() != 1 {
		t.Fatalf("finalizer calls = %d", finalized.Load())
	}
	batches := journal.snapshot()
	if len(batches) != 2 || len(batches[1]) != 3 ||
		batches[1][0].EventKind() != session.EventProviderNativeCommit ||
		batches[1][1].EventKind() != session.EventSampleUsage ||
		batches[1][2].EventKind() != session.EventTurnCompleted {
		t.Fatalf("durable batches = %#v", batches)
	}
	if _, err := collectTurn(runtime, context.Background(), nil); !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
		t.Fatalf("next RunTurn() error = %v", err)
	}
}

func TestRunTurnRevalidatesPreparedSampleBeforeNativeCommit(t *testing.T) {
	t.Parallel()
	finalized := atomic.Int32{}
	sample := newPreparedSample(t, func() { finalized.Add(1) })
	completed := completedProviderEventWithSample(t, sample)
	if err := sample.Finalize(); err != nil {
		t.Fatal(err)
	}
	journal := &fakeJournal{}
	runtime := newTestRuntime(t, &fakeConversation{stream: fixedStream(completed)}, journal)
	events, err := collectTurn(runtime, context.Background(), nil)
	if !errors.Is(err, &fault.Error{Code: fault.CodeStreamProtocol}) {
		t.Fatalf("RunTurn() error = %v", err)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
	batches := journal.snapshot()
	if len(batches) != 2 || len(batches[1]) != 1 || batches[1][0].EventKind() != session.EventTurnFailed {
		t.Fatalf("invalid sample persisted non-failure records: %#v", batches)
	}
	if finalized.Load() != 1 {
		t.Fatalf("runtime called finalized sample finalizer again: %d", finalized.Load())
	}
}

func TestRunTurnPersistsProviderFailureCancellationAndEarlyEOF(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		name   string
		stream func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error)
		code   fault.Code
	}{
		{name: "provider failure", stream: fixedStream(failedProviderEvent(t, fault.New(fault.CodeProviderRequest, "provider request failed"))), code: fault.CodeProviderRequest},
		{name: "cancelled", stream: fixedStream(cancelledProviderEvent(t, context.Canceled)), code: fault.CodeUserCancelled},
		{name: "early EOF", stream: fixedStream(), code: fault.CodeStreamProtocol},
		{name: "stream start failure", stream: func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
			return nil, fault.New(fault.CodeStreamIdleTimeout, "provider stream idle timeout")
		}, code: fault.CodeStreamIdleTimeout},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			journal := &fakeJournal{}
			runtime := newTestRuntime(t, &fakeConversation{stream: fixture.stream}, journal)
			events, err := collectTurn(runtime, context.Background(), nil)
			if !errors.Is(err, &fault.Error{Code: fixture.code}) {
				t.Fatalf("RunTurn() error = %v", err)
			}
			assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
			batches := journal.snapshot()
			if len(batches) != 2 || batches[1][0].EventKind() != session.EventTurnFailed {
				t.Fatalf("journal batches = %#v", batches)
			}
			failure, decodeErr := session.DecodeTurnFailedPayload(session.Record{
				PayloadVersion: 1, ReplayRequirement: session.ReplayRequired,
				EventKind: batches[1][0].EventKind(), Payload: batches[1][0].PayloadBytes(),
			})
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if failure.Code != string(fixture.code) {
				t.Fatalf("failure payload = %#v", failure)
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
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
	if finalized.Load() != 0 || len(journal.snapshot()) != 2 {
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
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
	batches := journal.snapshot()
	if len(batches) != 2 || len(batches[1]) != 1 || batches[1][0].EventKind() != session.EventTurnFailed {
		t.Fatalf("durable batches = %#v", batches)
	}
}

type runtimeTestNativeItem struct{}

func (runtimeTestNativeItem) ProviderFamily() domain.ProviderFamily { return domain.ProviderOpenAI }
func (runtimeTestNativeItem) ItemKind() string                      { return "message" }

func TestRunTurnRejectsEveryEventKindAfterTerminalAndDrains(t *testing.T) {
	t.Parallel()
	delta, err := protocol.NewAssistantTextDelta("late")
	if err != nil {
		t.Fatal(err)
	}
	nativeTail, err := provider.NewNativeStreamEvent(runtimeTestNativeItem{})
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		name string
		tail func(*testing.T) provider.StreamEvent
	}{
		{name: "semantic", tail: func(t *testing.T) provider.StreamEvent { return semanticProviderEvent(t, delta) }},
		{name: "native", tail: func(*testing.T) provider.StreamEvent { return nativeTail }},
		{name: "terminal", tail: func(t *testing.T) provider.StreamEvent {
			return failedProviderEvent(t, fault.New(fault.CodeProviderRequest, "late failure"))
		}},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			var finalized atomic.Int32
			prepared := newPreparedSample(t, func() { finalized.Add(1) })
			producerDone := make(chan struct{})
			cancelObserved := make(chan struct{})
			conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
				stream := make(chan provider.StreamEvent)
				go func() {
					defer close(producerDone)
					defer close(stream)
					stream <- completedProviderEventWithSample(t, prepared)
					stream <- fixture.tail(t)
					<-ctx.Done()
					close(cancelObserved)
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
			assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
			if finalized.Load() != 0 {
				t.Fatalf("finalizer calls = %d", finalized.Load())
			}
		})
	}
}

func TestRunTurnCancellationAndCompletionHaveDeterministicOwners(t *testing.T) {
	t.Run("cancellation wins before terminal", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancelObserved := make(chan struct{})
		releaseCleanup := make(chan struct{})
		producerDone := make(chan struct{})
		conversation := &fakeConversation{stream: func(streamCtx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
			stream := make(chan provider.StreamEvent)
			go func() {
				defer close(producerDone)
				defer close(stream)
				<-streamCtx.Done()
				close(cancelObserved)
				stream <- cancelledProviderEvent(t, streamCtx.Err())
				<-releaseCleanup
			}()
			return stream, nil
		}}
		runtime := newTestRuntime(t, conversation, &fakeJournal{})
		runDone := make(chan error, 1)
		go func() {
			runDone <- runtime.RunTurn(ctx, provider.TurnInput{Text: "hello"}, func(protocol.Event) {})
		}()
		cancel()
		<-cancelObserved
		select {
		case err := <-runDone:
			t.Fatalf("RunTurn returned before producer cleanup: %v", err)
		default:
		}
		close(releaseCleanup)
		if err := <-runDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("RunTurn() error = %v", err)
		}
		<-producerDone
	})

	t.Run("completion wins before cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		terminalSent := make(chan struct{})
		releaseCleanup := make(chan struct{})
		producerDone := make(chan struct{})
		var finalized atomic.Int32
		prepared := newPreparedSample(t, func() { finalized.Add(1) })
		conversation := &fakeConversation{stream: func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
			stream := make(chan provider.StreamEvent)
			go func() {
				defer close(producerDone)
				defer close(stream)
				stream <- completedProviderEventWithSample(t, prepared)
				close(terminalSent)
				<-releaseCleanup
			}()
			return stream, nil
		}}
		runtime := newTestRuntime(t, conversation, &fakeJournal{})
		runDone := make(chan error, 1)
		go func() {
			runDone <- runtime.RunTurn(ctx, provider.TurnInput{Text: "hello"}, func(protocol.Event) {})
		}()
		<-terminalSent
		cancel()
		select {
		case err := <-runDone:
			t.Fatalf("RunTurn returned before producer cleanup: %v", err)
		default:
		}
		close(releaseCleanup)
		if err := <-runDone; err != nil {
			t.Fatalf("RunTurn() error = %v", err)
		}
		<-producerDone
		if finalized.Load() != 1 {
			t.Fatalf("finalizer calls = %d", finalized.Load())
		}
	})
}

func TestRunTurnRejectsConcurrentTurn(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	prepared := newPreparedSample(t, func() {})
	conversation := &fakeConversation{stream: func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		close(started)
		go func() {
			<-release
			stream <- completedProviderEventWithSample(t, prepared)
			close(stream)
		}()
		return stream, nil
	}}
	runtime := newTestRuntime(t, conversation, &fakeJournal{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- runtime.RunTurn(context.Background(), provider.TurnInput{Text: "first"}, func(protocol.Event) {})
	}()
	<-started
	secondErr := runtime.RunTurn(context.Background(), provider.TurnInput{Text: "second"}, func(protocol.Event) {})
	if !errors.Is(secondErr, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("second turn error = %v", secondErr)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first turn error = %v", err)
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
			_ = runtime.RunTurn(ctx, provider.TurnInput{Text: "hello"}, func(event protocol.Event) {
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

func TestNewRejectsInterruptedOrInvalidRuntimeState(t *testing.T) {
	t.Parallel()
	conversation := &fakeConversation{stream: fixedStream()}
	valid := testRuntimeConfig(t, &fakeJournal{})
	fixtures := []Config{
		{ThreadID: runtimeThreadID, Journal: &fakeJournal{}},
		{SessionID: runtimeSessionID, ThreadID: runtimeThreadID},
		{SessionID: runtimeSessionID, ThreadID: runtimeThreadID, Journal: &fakeJournal{}, InterruptedTail: true},
	}
	for _, fixture := range fixtures {
		if _, err := New(conversation, fixture); err == nil {
			t.Fatalf("New(%#v) unexpectedly succeeded", fixture)
		}
	}
	if _, err := New(conversation, valid); err != nil {
		t.Fatalf("New(valid) error = %v", err)
	}
	missingPlanner := valid
	missingPlanner.ContextPlanner = nil
	if _, err := New(conversation, missingPlanner); err == nil {
		t.Fatal("missing context planner unexpectedly accepted")
	}
	invalidProjectInstructions := valid
	invalidProjectInstructions.ProjectInstructions = domain.ProjectInstructionsSnapshot{}
	if _, err := New(conversation, invalidProjectInstructions); err == nil {
		t.Fatal("invalid project instructions unexpectedly accepted")
	}
	wrongProfile, err := contextplan.NewProviderProfile(domain.ProviderAnthropic, "claude-test")
	if err != nil {
		t.Fatal(err)
	}
	mismatched := valid
	mismatched.ContextProfile = wrongProfile
	if _, err := New(conversation, mismatched); err == nil {
		t.Fatal("mismatched context profile unexpectedly accepted")
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
		ContextProfile: profile, ToolCatalog: mustRuntimeToolCatalog(t), ProjectInstructions: projectInstructions,
		ReadExecutor:   runtimeReadExecutor{},
		ContextBudget:  contextplan.DisabledBudget(),
		ContextPlanner: contextplan.NewPlanner(),
	}
}

func mustRuntimeToolCatalog(t *testing.T) tool.CatalogSnapshot {
	t.Helper()
	return mustRuntimeToolCatalogWithExecutor(t, runtimeReadExecutor{})
}

func mustRuntimeToolCatalogWithExecutor(t *testing.T, executor tool.ReadExecutor) tool.CatalogSnapshot {
	t.Helper()
	catalog, err := tool.NewReadCatalogSnapshot(executor)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
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
	call, err := tool.NewReadyCall(callID, input)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func testRuntimeInvocation(t *testing.T, invocationValue string, callValue string) tool.ReadInvocation {
	t.Helper()
	invocationID, err := tool.ParseInvocationID(invocationValue)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewReadInvocation(invocationID, testRuntimeReadyCall(t, callValue))
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func testRuntimeProjectInstructions(t *testing.T, source string, content string) domain.ProjectInstructionsSnapshot {
	t.Helper()
	document, err := domain.NewProjectInstructionDocument(source, content)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewProjectInstructionsSnapshot([]domain.ProjectInstructionDocument{document}, 32<<10)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func newPreparedSample(t *testing.T, finalize func()) *provider.PreparedSample {
	t.Helper()
	envelope, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, "responses", 1, json.RawMessage(`{"shape":"text_sample"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.NewPreparedSample(envelope, testRuntimeUsage(t), finalize)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func newPreparedSampleWithReady(t *testing.T, ready ...tool.ReadyCall) *provider.PreparedSample {
	t.Helper()
	envelope, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, "responses", 1, json.RawMessage(`{"shape":"call_sample"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.NewPreparedSample(envelope, testRuntimeUsage(t), func() {}, ready...)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func decodeRuntimeResultDraft(
	t *testing.T,
	draft session.RecordDraft,
	invocation tool.ReadInvocation,
) tool.InvocationResult {
	t.Helper()
	payload, err := session.DecodeToolCallResultPayload(session.Record{
		PayloadVersion: 1, ReplayRequirement: session.ReplayRequired,
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
		PayloadVersion: 1, ReplayRequirement: session.ReplayRequired,
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

func failedProviderEvent(t *testing.T, failure error) provider.StreamEvent {
	t.Helper()
	event, err := provider.NewFailedStreamEvent(failure)
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
	err := runtime.RunTurn(ctx, provider.TurnInput{Text: "hello"}, func(event protocol.Event) {
		events = append(events, event)
		if observe != nil {
			observe(event)
		}
	})
	return events, err
}

func assertEventKinds(t *testing.T, events []protocol.Event, want ...protocol.EventKind) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("event count: got %d want %d (%#v)", len(events), len(want), events)
	}
	for index := range want {
		if events[index].Kind != want[index] {
			t.Fatalf("event %d: got %s want %s", index, events[index].Kind, want[index])
		}
	}
}

func TestChatSessionInterruptAndShutdownLifecycle(t *testing.T) {
	t.Parallel()
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		go func() {
			<-ctx.Done()
			stream <- cancelledProviderEvent(t, context.Canceled)
			close(stream)
		}()
		return stream, nil
	}}
	sessionFacade := NewChatSession(newTestRuntime(t, conversation, &fakeJournal{}))
	events, err := sessionFacade.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Kind != protocol.EventTurnStarted {
		t.Fatalf("first event = %s", event.Kind)
	}
	sessionFacade.Interrupt()
	sessionFacade.Interrupt()
	var terminal protocol.Event
	for event := range events {
		terminal = event
	}
	if terminal.Kind != protocol.EventTurnFailed {
		t.Fatalf("terminal = %#v", terminal)
	}
	payload, err := protocol.DecodeTurnFailed(terminal)
	if err != nil || !payload.Cancelled {
		t.Fatalf("cancel payload = %#v, %v", payload, err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sessionFacade.Shutdown(shutdownContext); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionFacade.Submit(context.Background(), "after close"); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("Submit(after close) error = %v", err)
	}
}

func TestChatSessionSubmitRequiresLiveContextBeforeAdmission(t *testing.T) {
	t.Parallel()
	conversation := &fakeConversation{stream: fixedStream()}
	sessionFacade := NewChatSession(newTestRuntime(t, conversation, &fakeJournal{}))
	var nilContext context.Context
	if _, err := sessionFacade.Submit(nilContext, "nil"); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("Submit(nil) error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sessionFacade.Submit(cancelled, "cancelled"); !errors.Is(err, &fault.Error{Code: fault.CodeUserCancelled}) {
		t.Fatalf("Submit(cancelled) error = %v", err)
	}
	if conversation.calls.Load() != 0 {
		t.Fatalf("rejected context made %d provider calls", conversation.calls.Load())
	}
	if err := sessionFacade.Shutdown(nilContext); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("Shutdown(nil) error = %v", err)
	}
}

func TestChatSessionPropagatesSubmitContextAndRejectsConcurrentTurn(t *testing.T) {
	t.Parallel()
	providerStarted := make(chan struct{})
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		close(providerStarted)
		go func() {
			<-ctx.Done()
			stream <- cancelledProviderEvent(t, ctx.Err())
			close(stream)
		}()
		return stream, nil
	}}
	sessionFacade := NewChatSession(newTestRuntime(t, conversation, &fakeJournal{}))
	ctx, cancel := context.WithCancel(context.Background())
	events, err := sessionFacade.Submit(ctx, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Kind != protocol.EventTurnStarted {
		t.Fatalf("first event = %s", event.Kind)
	}
	<-providerStarted
	if _, err := sessionFacade.Submit(context.Background(), "concurrent"); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("concurrent Submit() error = %v", err)
	}
	cancel()
	var terminal protocol.Event
	for event := range events {
		terminal = event
	}
	if terminal.Kind != protocol.EventTurnFailed {
		t.Fatalf("terminal = %#v", terminal)
	}
	payload, err := protocol.DecodeTurnFailed(terminal)
	if err != nil || !payload.Cancelled {
		t.Fatalf("cancel payload = %#v, %v", payload, err)
	}
	if err := sessionFacade.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
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

func TestChatSessionConcurrentInterruptAndShutdown(t *testing.T) {
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		go func() {
			<-ctx.Done()
			stream <- cancelledProviderEvent(t, context.Canceled)
			close(stream)
		}()
		return stream, nil
	}}
	sessionFacade := NewChatSession(newTestRuntime(t, conversation, &fakeJournal{}))
	events, err := sessionFacade.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Kind != protocol.EventTurnStarted {
		t.Fatalf("first event = %s", event.Kind)
	}
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			sessionFacade.Interrupt()
		}()
	}
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := sessionFacade.Shutdown(ctx); err != nil {
				t.Errorf("Shutdown() error = %v", err)
			}
		}()
	}
	wait.Wait()
	for range events {
	}
	if _, err := sessionFacade.Submit(context.Background(), "closed"); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("Submit(closed) error = %v", err)
	}
}

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

const (
	runtimeSessionID = domain.SessionID("00000000-0010-7000-8000-000000000010")
	runtimeThreadID  = domain.ThreadID("00000000-0011-7000-8000-000000000011")
	runtimeTurnID    = domain.TurnID("00000000-0012-7000-8000-000000000012")
)

type fakeConversation struct {
	stream func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error)
	calls  atomic.Int32
}

func (*fakeConversation) Family() domain.ProviderFamily { return domain.ProviderOpenAI }

func (*fakeConversation) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (*fakeConversation) ProjectHistory() domain.SemanticHistoryView {
	return domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: make([]domain.SemanticTurn, 0)}
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
			provider.StreamEvent{Kind: provider.StreamEventSemantic, Event: delta},
			provider.StreamEvent{Kind: provider.StreamEventCompleted, Prepared: prepared},
		)(context.Background(), provider.TurnInput{})
	}}
	journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
		addTimeline("append:" + string(drafts[0].EventKind))
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
	if len(batches) != 2 || len(batches[0]) != 1 || len(batches[1]) != 2 ||
		batches[1][0].EventKind != session.EventProviderNativeCommit ||
		batches[1][1].EventKind != session.EventTurnCompleted {
		t.Fatalf("journal batches = %#v", batches)
	}
	commit := batches[1][0].Payload.(session.NativeCommitPayload)
	if commit.Provider != domain.ProviderOpenAI || commit.Wire != "responses" || string(commit.Payload) != `{"shape":"text_sample"}` {
		t.Fatalf("native commit = %#v", commit)
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

func TestRunTurnCompletionWriteFailureDoesNotFinalizeAndPoisons(t *testing.T) {
	t.Parallel()
	var finalized atomic.Int32
	prepared := newPreparedSample(t, func() { finalized.Add(1) })
	conversation := &fakeConversation{stream: fixedStream(provider.StreamEvent{
		Kind: provider.StreamEventCompleted, Prepared: prepared,
	})}
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

func TestRunTurnPersistsProviderFailureCancellationAndEarlyEOF(t *testing.T) {
	t.Parallel()
	fixtures := []struct {
		name   string
		stream func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error)
		code   fault.Code
	}{
		{name: "provider failure", stream: fixedStream(provider.StreamEvent{Kind: provider.StreamEventFailed, Err: fault.New(fault.CodeProviderRequest, "provider request failed")}), code: fault.CodeProviderRequest},
		{name: "cancelled", stream: fixedStream(provider.StreamEvent{Kind: provider.StreamEventCancelled, Err: context.Canceled}), code: fault.CodeUserCancelled},
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
			if len(batches) != 2 || batches[1][0].EventKind != session.EventTurnFailed {
				t.Fatalf("journal batches = %#v", batches)
			}
			failure := batches[1][0].Payload.(session.TurnFailedPayload)
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
		provider.StreamEvent{Kind: provider.StreamEventCompleted, Prepared: prepared},
		provider.StreamEvent{Kind: provider.StreamEventSemantic, Event: delta},
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
			stream <- provider.StreamEvent{Kind: provider.StreamEventCompleted, Prepared: prepared}
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
					stream <- provider.StreamEvent{Kind: provider.StreamEventCancelled, Err: context.Canceled}
				default:
					stream <- provider.StreamEvent{Kind: provider.StreamEventCompleted, Prepared: prepared}
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
	valid := Config{SessionID: runtimeSessionID, ThreadID: runtimeThreadID, Journal: &fakeJournal{}}
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
}

func newTestRuntime(t *testing.T, conversation provider.Conversation, journal Journal) *Runtime {
	t.Helper()
	runtime, err := New(conversation, Config{
		SessionID: runtimeSessionID, ThreadID: runtimeThreadID, Journal: journal,
		NewTurnID: func() (domain.TurnID, error) { return runtimeTurnID, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func newPreparedSample(t *testing.T, finalize func()) *provider.PreparedSample {
	t.Helper()
	envelope, err := provider.NewNativeCommitEnvelope(
		domain.ProviderOpenAI, "responses", 1, json.RawMessage(`{"shape":"text_sample"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.NewPreparedSample(envelope, finalize)
	if err != nil {
		t.Fatal(err)
	}
	return prepared
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
			stream <- provider.StreamEvent{Kind: provider.StreamEventCancelled, Err: context.Canceled}
			close(stream)
		}()
		return stream, nil
	}}
	sessionFacade := NewChatSession(newTestRuntime(t, conversation, &fakeJournal{}))
	events, err := sessionFacade.Submit("hello")
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
	if _, err := sessionFacade.Submit("after close"); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("Submit(after close) error = %v", err)
	}
}

func TestChatSessionShutdownWaitsForDurableFailure(t *testing.T) {
	t.Parallel()
	blocked := make(chan struct{})
	release := make(chan struct{})
	var blockOnce sync.Once
	journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
		if drafts[0].EventKind != session.EventTurnFailed {
			return
		}
		blockOnce.Do(func() { close(blocked) })
		<-release
	}}
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		go func() {
			<-ctx.Done()
			stream <- provider.StreamEvent{Kind: provider.StreamEventCancelled, Err: context.Canceled}
			close(stream)
		}()
		return stream, nil
	}}
	sessionFacade := NewChatSession(newTestRuntime(t, conversation, journal))
	events, err := sessionFacade.Submit("hello")
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Kind != protocol.EventTurnStarted {
		t.Fatalf("first event = %s", event.Kind)
	}
	sessionFacade.Interrupt()
	<-blocked
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- sessionFacade.Shutdown(context.Background()) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("Shutdown returned before journal completion: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
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
			stream <- provider.StreamEvent{Kind: provider.StreamEventCancelled, Err: context.Canceled}
			close(stream)
		}()
		return stream, nil
	}}
	sessionFacade := NewChatSession(newTestRuntime(t, conversation, &fakeJournal{}))
	events, err := sessionFacade.Submit("hello")
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
	if _, err := sessionFacade.Submit("closed"); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("Submit(closed) error = %v", err)
	}
}

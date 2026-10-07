package runtime

import (
	"context"
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
)

type controlledInvocation struct {
	input   string
	ctx     context.Context
	release chan provider.StreamEvent
}

type controlledConversation struct {
	calls          chan controlledInvocation
	cancelObserved chan struct{}
	cancelRelease  <-chan struct{}
	active         atomic.Int32
	maxActive      atomic.Int32
}

func newControlledConversation() *controlledConversation {
	return &controlledConversation{calls: make(chan controlledInvocation, 16)}
}

func (*controlledConversation) Family() domain.ProviderFamily { return domain.ProviderOpenAI }

func (*controlledConversation) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (*controlledConversation) ProjectHistory() domain.SemanticHistoryView {
	return domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: make([]domain.SemanticTurn, 0)}
}

func (*controlledConversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	estimateValue, _ := domain.NewEstimatedTokenEstimate("byte_heuristic_v1", 0)
	return domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 0, estimateValue)
}

func (conversation *controlledConversation) Stream(ctx context.Context, input provider.TurnInput) (<-chan provider.StreamEvent, error) {
	active := conversation.active.Add(1)
	for {
		maximum := conversation.maxActive.Load()
		if active <= maximum || conversation.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	stream := make(chan provider.StreamEvent, 1)
	invocation := controlledInvocation{input: input.Text, ctx: ctx, release: make(chan provider.StreamEvent, 1)}
	conversation.calls <- invocation
	go func() {
		defer conversation.active.Add(-1)
		select {
		case event := <-invocation.release:
			stream <- event
		case <-ctx.Done():
			if conversation.cancelObserved != nil {
				select {
				case conversation.cancelObserved <- struct{}{}:
				default:
				}
			}
			if conversation.cancelRelease != nil {
				<-conversation.cancelRelease
			}
			event, err := provider.NewCancelledStreamEvent(ctx.Err())
			if err == nil {
				stream <- event
			}
		}
		close(stream)
	}()
	return stream, nil
}

func TestAgentLoopConstructionAndExplicitLifecycle(t *testing.T) {
	t.Parallel()
	if _, err := NewAgentLoop(nil, DefaultAgentLoopConfig()); err == nil {
		t.Fatal("nil Runtime was accepted")
	}
	invalid := DefaultAgentLoopConfig()
	invalid.MaxQueuedInputs = 0
	if _, err := NewAgentLoop(newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{}), invalid); err == nil {
		t.Fatal("invalid config was accepted")
	}
	loop, err := NewAgentLoop(newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{}), DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	shutdown, _ := protocol.NewShutdownCommand("shutdown-before-run")
	if _, err := loop.Submit(context.Background(), shutdown); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("Submit(before Run) error = %v", err)
	}
	var nilContext context.Context
	if err := loop.Run(nilContext); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("Run(nil) error = %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- loop.Run(context.Background()) }()
	<-loop.Ready()
	if err := loop.Run(context.Background()); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("second Run() error = %v", err)
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	<-loop.Done()
	if err := loop.Stop(context.Background()); err != nil {
		t.Fatalf("Stop(after close) error = %v", err)
	}
}

func TestAgentLoopAcceptedCommandResultWinsClosedDone(t *testing.T) {
	command, err := protocol.NewShutdownCommand("shutdown-result-race")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionClosing)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan loopCommandResponse, 1)
	done := make(chan struct{})
	results <- loopCommandResponse{result: accepted}
	close(done)

	result, err := waitLoopCommandResponse(results, done)
	if err != nil || result.Disposition() != protocol.CommandDispositionClosing {
		t.Fatalf("waitLoopCommandResponse() = %#v, %v", result, err)
	}
}

func TestAgentLoopFIFOQueueCapacityDedupeAndDrain(t *testing.T) {
	conversation := newControlledConversation()
	config := testRuntimeConfig(t, &fakeJournal{})
	config.GenerateTurnID = sequentialTurnIDs()
	runtimeValue, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	loopConfig := DefaultAgentLoopConfig()
	loopConfig.MaxQueuedInputs = 2
	loopConfig.MaxQueuedBytes = 12
	loopConfig.RecentRequests = 4
	loop, err := NewAgentLoop(runtimeValue, loopConfig)
	if err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- loop.Run(context.Background()) }()
	<-loop.Ready()

	firstResult := submitLoopCommand(t, loop, "input-1", "first")
	if firstResult.Disposition() != protocol.CommandDispositionStarting {
		t.Fatalf("first disposition = %s", firstResult.Disposition())
	}
	firstCall := receiveInvocation(t, conversation.calls)
	secondResult := submitLoopCommand(t, loop, "input-2", "second")
	thirdResult := submitLoopCommand(t, loop, "input-3", "third3")
	if secondResult.Disposition() != protocol.CommandDispositionQueued || thirdResult.Disposition() != protocol.CommandDispositionQueued {
		t.Fatalf("queued dispositions = %s/%s", secondResult.Disposition(), thirdResult.Disposition())
	}
	duplicate := submitLoopCommand(t, loop, "input-2", "second")
	assertRejectedCode(t, duplicate, protocol.ControlErrorDuplicateRequest)
	overflow := submitLoopCommand(t, loop, "input-4", "x")
	assertRejectedCode(t, overflow, protocol.ControlErrorInputQueueFull)
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}

	firstCall.release <- completedProviderEvent(t)
	secondCall := receiveInvocation(t, conversation.calls)
	secondCall.release <- completedProviderEvent(t)
	thirdCall := receiveInvocation(t, conversation.calls)
	thirdCall.release <- completedProviderEvent(t)
	if got := []string{firstCall.input, secondCall.input, thirdCall.input}; fmt.Sprint(got) != "[first second third3]" {
		t.Fatalf("provider input order = %v", got)
	}
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	items := drainControlItems(loop.Outputs())
	assertTurnOrdering(t, items, []protocol.RequestID{"input-1", "input-2", "input-3"})
	if conversation.maxActive.Load() != 1 || conversation.active.Load() != 0 {
		t.Fatalf("provider concurrency max=%d active=%d", conversation.maxActive.Load(), conversation.active.Load())
	}
}

func TestAgentLoopSerializesConcurrentIdleSubmissions(t *testing.T) {
	conversation := newControlledConversation()
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	runDone := startAgentLoop(t, loop)
	start := make(chan struct{})
	type concurrentResult struct {
		result protocol.CommandResult
		err    error
	}
	results := make(chan concurrentResult, 2)
	for index := 1; index <= 2; index++ {
		index := index
		command, err := protocol.NewSubmitInputCommand(protocol.RequestID(fmt.Sprintf("concurrent-%d", index)), fmt.Sprintf("input-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			<-start
			result, submitErr := loop.Submit(context.Background(), command)
			results <- concurrentResult{result: result, err: submitErr}
		}()
	}
	close(start)
	firstResponse, secondResponse := <-results, <-results
	if firstResponse.err != nil || secondResponse.err != nil {
		t.Fatalf("concurrent Submit() errors = %v/%v", firstResponse.err, secondResponse.err)
	}
	starting, queued := 0, 0
	for _, result := range []protocol.CommandResult{firstResponse.result, secondResponse.result} {
		switch result.Disposition() {
		case protocol.CommandDispositionStarting:
			starting++
		case protocol.CommandDispositionQueued:
			queued++
		}
	}
	if starting != 1 || queued != 1 {
		t.Fatalf("concurrent dispositions: starting=%d queued=%d", starting, queued)
	}
	first := receiveInvocation(t, conversation.calls)
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.release <- completedProviderEvent(t)
	second := receiveInvocation(t, conversation.calls)
	second.release <- completedProviderEvent(t)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	if first.input == second.input || conversation.maxActive.Load() != 1 {
		t.Fatalf("concurrent execution = %q/%q max=%d", first.input, second.input, conversation.maxActive.Load())
	}
}

func TestAgentLoopCancelledAdmissionAndIdleInterruptHaveNoSideEffects(t *testing.T) {
	conversation := newControlledConversation()
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	runDone := startAgentLoop(t, loop)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	command, _ := protocol.NewSubmitInputCommand("cancelled-input", "must not run")
	if _, err := loop.Submit(cancelled, command); !errors.Is(err, &fault.Error{Code: fault.CodeUserCancelled}) {
		t.Fatalf("cancelled Submit() error = %v", err)
	}
	interrupt, _ := protocol.NewInterruptCommand("idle-interrupt", domain.TurnID("00000000-0099-7000-8000-000000000099"))
	result, err := loop.Submit(context.Background(), interrupt)
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, result, protocol.ControlErrorNoActiveTurn)
	if conversation.callsRemaining() != 0 {
		t.Fatalf("rejected commands made %d Provider calls", conversation.callsRemaining())
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
}

func TestAgentLoopContinuesAfterOrdinaryFailureAndStopsWhenPoisoned(t *testing.T) {
	t.Run("ordinary failure", func(t *testing.T) {
		conversation := newControlledConversation()
		loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
		runDone := startAgentLoop(t, loop)
		submitLoopCommand(t, loop, "ordinary-1", "first")
		first := receiveInvocation(t, conversation.calls)
		submitLoopCommand(t, loop, "ordinary-2", "second")
		if err := loop.CloseInput(context.Background()); err != nil {
			t.Fatal(err)
		}
		first.release <- failedProviderEvent(t, fault.New(fault.CodeProviderRequest, "provider request failed"))
		second := receiveInvocation(t, conversation.calls)
		second.release <- completedProviderEvent(t)
		if err := <-runDone; err != nil {
			t.Fatalf("ordinary turn failure stopped loop: %v", err)
		}
		items := drainControlItems(loop.Outputs())
		assertTurnOrdering(t, items, []protocol.RequestID{"ordinary-1", "ordinary-2"})
	})

	t.Run("poisoned journal", func(t *testing.T) {
		conversation := newControlledConversation()
		journal := &fakeJournal{failAt: 2}
		loop := newControlledAgentLoop(t, conversation, journal)
		runDone := startAgentLoop(t, loop)
		submitLoopCommand(t, loop, "poison-1", "first")
		first := receiveInvocation(t, conversation.calls)
		submitLoopCommand(t, loop, "poison-2", "second")
		first.release <- completedProviderEvent(t)
		if err := <-runDone; !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
			t.Fatalf("Run() error = %v", err)
		}
		items := drainControlItems(loop.Outputs())
		if !containsDiscard(items, "poison-2", protocol.ControlErrorSessionFailed) {
			t.Fatalf("missing poisoned discard: %#v", items)
		}
		if conversation.callsRemaining() != 0 || conversation.maxActive.Load() != 1 {
			t.Fatalf("poisoned queue started Provider: calls=%d", conversation.callsRemaining())
		}
	})
}

func TestAgentLoopTargetedInterruptIsRaceSafe(t *testing.T) {
	conversation := newControlledConversation()
	cancelObserved := make(chan struct{}, 1)
	cancelRelease := make(chan struct{})
	conversation.cancelObserved = cancelObserved
	conversation.cancelRelease = cancelRelease
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	runDone := startAgentLoop(t, loop)
	submitLoopCommand(t, loop, "interrupt-input-1", "first")
	firstCall := receiveInvocation(t, conversation.calls)
	started := receiveTurnEvent(t, loop.Outputs(), "interrupt-input-1", protocol.EventTurnStarted)
	submitLoopCommand(t, loop, "interrupt-input-2", "second")

	stale, _ := protocol.NewInterruptCommand("interrupt-stale", domain.TurnID("00000000-0099-7000-8000-000000000099"))
	staleResult, err := loop.Submit(context.Background(), stale)
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, staleResult, protocol.ControlErrorTurnMismatch)
	select {
	case <-firstCall.ctx.Done():
		t.Fatal("stale interrupt cancelled active turn")
	default:
	}

	valid, _ := protocol.NewInterruptCommand("interrupt-valid", started.TurnID)
	validResult, err := loop.Submit(context.Background(), valid)
	if err != nil || validResult.Disposition() != protocol.CommandDispositionInterrupting {
		t.Fatalf("valid interrupt = %#v, %v", validResult, err)
	}
	<-cancelObserved
	repeated, _ := protocol.NewInterruptCommand("interrupt-repeat", started.TurnID)
	repeatedResult, err := loop.Submit(context.Background(), repeated)
	if err != nil || repeatedResult.Disposition() != protocol.CommandDispositionInterrupting {
		t.Fatalf("repeated interrupt = %#v, %v", repeatedResult, err)
	}
	<-firstCall.ctx.Done()
	close(cancelRelease)
	secondCall := receiveInvocation(t, conversation.calls)
	secondStarted := receiveTurnEvent(t, loop.Outputs(), "interrupt-input-2", protocol.EventTurnStarted)
	if secondStarted.TurnID == started.TurnID {
		t.Fatal("follow-up reused turn ID")
	}
	staleAgain, _ := protocol.NewInterruptCommand("interrupt-old-target", started.TurnID)
	staleAgainResult, err := loop.Submit(context.Background(), staleAgain)
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, staleAgainResult, protocol.ControlErrorTurnMismatch)
	select {
	case <-secondCall.ctx.Done():
		t.Fatal("old target cancelled the new turn")
	default:
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	secondCall.release <- completedProviderEvent(t)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	items := drainControlItems(loop.Outputs())
	if terminalCount(items, "interrupt-input-2") != 1 || conversation.maxActive.Load() != 1 {
		t.Fatalf("terminal counts are invalid: %#v", items)
	}
}

func TestAgentLoopShutdownDiscardsQueueAndRetainsCleanupOwner(t *testing.T) {
	providerCancelled := make(chan struct{})
	releaseCleanup := make(chan struct{})
	var cancelOnce sync.Once
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		go func() {
			<-ctx.Done()
			cancelOnce.Do(func() { close(providerCancelled) })
			<-releaseCleanup
			stream <- cancelledProviderEvent(t, ctx.Err())
			close(stream)
		}()
		return stream, nil
	}}
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	runDone := startAgentLoop(t, loop)
	submitLoopCommand(t, loop, "shutdown-input-1", "first")
	receiveTurnEvent(t, loop.Outputs(), "shutdown-input-1", protocol.EventTurnStarted)
	submitLoopCommand(t, loop, "shutdown-input-2", "second")
	shutdown, _ := protocol.NewShutdownCommand("shutdown-command")
	result, err := loop.Submit(context.Background(), shutdown)
	if err != nil || result.Disposition() != protocol.CommandDispositionClosing {
		t.Fatalf("shutdown result = %#v, %v", result, err)
	}
	repeatedShutdown, _ := protocol.NewShutdownCommand("shutdown-command-repeat")
	repeatedResult, err := loop.Submit(context.Background(), repeatedShutdown)
	if err != nil || repeatedResult.Disposition() != protocol.CommandDispositionClosing {
		t.Fatalf("repeated shutdown result = %#v, %v", repeatedResult, err)
	}
	<-providerCancelled

	waitContext, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if err := loop.Stop(waitContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("timed-out Stop() error = %v", err)
	}
	select {
	case <-loop.Done():
		t.Fatal("owner exited before Provider cleanup")
	default:
	}
	close(releaseCleanup)
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	items := drainControlItems(loop.Outputs())
	if !containsDiscard(items, "shutdown-input-2", protocol.ControlErrorSessionShutdown) {
		t.Fatalf("missing shutdown discard: %#v", items)
	}
	if terminalCount(items, "shutdown-input-1") != 1 || conversation.calls.Load() != 1 {
		t.Fatalf("shutdown terminal/calls = %d/%d", terminalCount(items, "shutdown-input-1"), conversation.calls.Load())
	}
}

func TestAgentLoopRunStateOwnsQueueLedgerAndClosingTransitions(t *testing.T) {
	t.Parallel()
	loop := &AgentLoop{
		config:  AgentLoopConfig{MaxQueuedInputs: 2, MaxQueuedBytes: 16, RecentRequests: 4},
		outputs: make(chan protocol.ControlItem, 4),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := newAgentLoopRunState(loop, ctx)
	state.queue = append(state.queue,
		queuedInput{requestID: "queued-1", text: "one"},
		queuedInput{requestID: "queued-2", text: "two"},
	)
	state.queuedBytes = 6
	state.outstanding["queued-1"] = struct{}{}
	state.outstanding["queued-2"] = struct{}{}
	state.discardQueue(protocol.ControlErrorSessionShutdown)
	if len(state.queue) != 0 || state.queuedBytes != 0 || len(state.outstanding) != 0 ||
		!state.recent.contains("queued-1") || !state.recent.contains("queued-2") || len(loop.outputs) != 2 {
		t.Fatalf("discarded state = queue %#v, bytes %d, outstanding %#v, recent %#v, outputs %d", state.queue, state.queuedBytes, state.outstanding, state.recent, len(loop.outputs))
	}

	activeContext, activeCancel := context.WithCancel(context.Background())
	state.active = &activeLoopTurn{cancel: activeCancel}
	accepted := make(chan struct{})
	state.handleLifecycle(loopLifecycleRequest{kind: loopLifecycleStop, accepted: accepted})
	if state.mode != loopModeClosing || !state.active.cancelRequested {
		t.Fatalf("closing state = mode %d, active %#v", state.mode, state.active)
	}
	select {
	case <-accepted:
	default:
		t.Fatal("lifecycle request was not acknowledged")
	}
	select {
	case <-activeContext.Done():
	default:
		t.Fatal("active turn was not cancelled")
	}
}

func TestAgentLoopWaitsForProviderChannelCloseBeforeFollowUp(t *testing.T) {
	t.Parallel()
	firstStarted := make(chan struct{})
	firstCancelled := make(chan struct{})
	releaseFirstCleanup := make(chan struct{})
	firstDone := make(chan struct{})
	secondStarted := make(chan struct{})
	var calls atomic.Int32
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent)
		switch calls.Add(1) {
		case 1:
			close(firstStarted)
			go func() {
				defer close(firstDone)
				defer close(stream)
				stream <- provider.StreamEvent{}
				<-ctx.Done()
				close(firstCancelled)
				<-releaseFirstCleanup
			}()
		case 2:
			close(secondStarted)
			completed := completedProviderEvent(t)
			go func() {
				stream <- completed
				close(stream)
			}()
		default:
			close(stream)
		}
		return stream, nil
	}}
	loop, err := NewAgentLoop(newTestRuntime(t, conversation, &fakeJournal{}), DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- loop.Run(context.Background()) }()
	<-loop.Ready()

	firstCommand, _ := protocol.NewSubmitInputCommand("cleanup-first", "first")
	firstResult, err := loop.Submit(context.Background(), firstCommand)
	if err != nil || firstResult.Disposition() != protocol.CommandDispositionStarting {
		t.Fatalf("first submit = %#v, %v", firstResult, err)
	}
	<-firstStarted
	secondCommand, _ := protocol.NewSubmitInputCommand("cleanup-second", "second")
	secondResult, err := loop.Submit(context.Background(), secondCommand)
	if err != nil || secondResult.Disposition() != protocol.CommandDispositionQueued {
		t.Fatalf("second submit = %#v, %v", secondResult, err)
	}
	<-firstCancelled
	select {
	case <-secondStarted:
		t.Fatal("follow-up started before provider channel closed")
	default:
	}
	close(releaseFirstCleanup)
	<-firstDone
	<-secondStarted
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	for range loop.Outputs() {
	}
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
}

func newControlledAgentLoop(t *testing.T, conversation provider.Conversation, journal Journal) *AgentLoop {
	t.Helper()
	config := testRuntimeConfig(t, journal)
	config.GenerateTurnID = sequentialTurnIDs()
	runtimeValue, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	loop, err := NewAgentLoop(runtimeValue, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	return loop
}

func startAgentLoop(t *testing.T, loop *AgentLoop) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- loop.Run(context.Background()) }()
	<-loop.Ready()
	return done
}

func sequentialTurnIDs() TurnIDGenerator {
	var sequence atomic.Uint32
	return func() (domain.TurnID, error) {
		value := sequence.Add(1)
		return domain.TurnID(fmt.Sprintf("00000000-%04x-7000-8000-%012x", value, value)), nil
	}
}

func submitLoopCommand(t *testing.T, loop *AgentLoop, requestID protocol.RequestID, text string) protocol.CommandResult {
	t.Helper()
	command, err := protocol.NewSubmitInputCommand(requestID, text)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Submit(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertRejectedCode(t *testing.T, result protocol.CommandResult, want protocol.ControlErrorCode) {
	t.Helper()
	summary, ok := result.Error()
	if result.Status() != protocol.CommandStatusRejected || !ok || summary.Code() != want {
		t.Fatalf("rejection = %#v/%#v, want %s", result, summary, want)
	}
}

func receiveInvocation(t *testing.T, calls <-chan controlledInvocation) controlledInvocation {
	t.Helper()
	select {
	case invocation := <-calls:
		return invocation
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Provider invocation")
		return controlledInvocation{}
	}
}

func completedProviderEvent(t *testing.T) provider.StreamEvent {
	t.Helper()
	return completedProviderEventWithSample(t, newPreparedSample(t, func() {}))
}

func receiveTurnEvent(t *testing.T, outputs <-chan protocol.ControlItem, requestID protocol.RequestID, kind protocol.EventKind) protocol.Event {
	t.Helper()
	for {
		select {
		case item, open := <-outputs:
			if !open {
				t.Fatal("control output closed before expected event")
			}
			event, ok := item.TurnEvent()
			if ok && item.RequestID() == requestID && event.Kind == kind {
				return event
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for %s/%s", requestID, kind)
		}
	}
}

func drainControlItems(outputs <-chan protocol.ControlItem) []protocol.ControlItem {
	var items []protocol.ControlItem
	for item := range outputs {
		items = append(items, item)
	}
	return items
}

func assertTurnOrdering(t *testing.T, items []protocol.ControlItem, inputs []protocol.RequestID) {
	t.Helper()
	position := make(map[protocol.RequestID]map[protocol.EventKind]int, len(inputs))
	for index, item := range items {
		event, ok := item.TurnEvent()
		if !ok {
			continue
		}
		if position[item.RequestID()] == nil {
			position[item.RequestID()] = make(map[protocol.EventKind]int)
		}
		position[item.RequestID()][event.Kind] = index
	}
	for index, input := range inputs {
		started, hasStarted := position[input][protocol.EventTurnStarted]
		terminal, completed := position[input][protocol.EventTurnCompleted]
		if !completed {
			terminal, completed = position[input][protocol.EventTurnFailed]
		}
		if !hasStarted || !completed || started >= terminal {
			t.Fatalf("invalid lifecycle for %s: %#v", input, position[input])
		}
		if index > 0 {
			previous := inputs[index-1]
			previousTerminal := position[previous][protocol.EventTurnCompleted]
			if failed, ok := position[previous][protocol.EventTurnFailed]; ok {
				previousTerminal = failed
			}
			if previousTerminal >= started {
				t.Fatalf("%s started before %s terminal", input, previous)
			}
		}
	}
}

func containsDiscard(items []protocol.ControlItem, requestID protocol.RequestID, code protocol.ControlErrorCode) bool {
	for _, item := range items {
		summary, ok := item.DiscardError()
		if ok && item.RequestID() == requestID && summary.Code() == code {
			return true
		}
	}
	return false
}

func terminalCount(items []protocol.ControlItem, requestID protocol.RequestID) int {
	count := 0
	for _, item := range items {
		event, ok := item.TurnEvent()
		if ok && item.RequestID() == requestID && (event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed) {
			count++
		}
	}
	return count
}

func (conversation *controlledConversation) callsRemaining() int {
	return len(conversation.calls)
}

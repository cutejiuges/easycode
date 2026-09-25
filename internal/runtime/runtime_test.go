package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

type fakeConversation struct {
	stream func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error)
}

func (fakeConversation) Family() domain.ProviderFamily {
	return domain.ProviderOpenAI
}

func (fakeConversation) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (conversation fakeConversation) Stream(ctx context.Context, input provider.TurnInput) (<-chan provider.StreamEvent, error) {
	return conversation.stream(ctx, input)
}

func TestRunTurnEmitsCompletedLifecycleInOrder(t *testing.T) {
	delta, err := protocol.NewAssistantTextDelta("hello")
	if err != nil {
		t.Fatalf("new delta: %v", err)
	}
	runtime := New(fakeConversation{stream: fixedStream(
		provider.StreamEvent{Kind: provider.StreamEventSemantic, Event: delta},
		provider.StreamEvent{Kind: provider.StreamEventCompleted},
	)})

	events, runErr := collectTurn(runtime, context.Background())
	if runErr != nil {
		t.Fatalf("run turn: %v", runErr)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventAssistantTextDelta, protocol.EventTurnCompleted)
}

func TestRunTurnMapsFailedTerminalOnce(t *testing.T) {
	providerErr := fault.New(fault.CodeProviderRequest, "provider request failed")
	runtime := New(fakeConversation{stream: fixedStream(
		provider.StreamEvent{Kind: provider.StreamEventFailed, Err: providerErr},
	)})

	events, runErr := collectTurn(runtime, context.Background())
	if !errors.Is(runErr, &fault.Error{Code: fault.CodeProviderRequest}) {
		t.Fatalf("run error: %v", runErr)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
	payload, err := protocol.DecodeTurnFailed(events[1])
	if err != nil || payload.Code != string(fault.CodeProviderRequest) || payload.Message != "provider request failed" {
		t.Fatalf("failure payload: %#v err=%v", payload, err)
	}
}

func TestRunTurnRejectsCloseBeforeTerminal(t *testing.T) {
	runtime := New(fakeConversation{stream: fixedStream()})
	events, runErr := collectTurn(runtime, context.Background())
	if !errors.Is(runErr, &fault.Error{Code: fault.CodeStreamProtocol}) {
		t.Fatalf("run error: %v", runErr)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
}

func TestRunTurnRejectsEventAfterTerminal(t *testing.T) {
	delta, err := protocol.NewAssistantTextDelta("late")
	if err != nil {
		t.Fatalf("new delta: %v", err)
	}
	runtime := New(fakeConversation{stream: fixedStream(
		provider.StreamEvent{Kind: provider.StreamEventCompleted},
		provider.StreamEvent{Kind: provider.StreamEventSemantic, Event: delta},
	)})
	events, runErr := collectTurn(runtime, context.Background())
	if !errors.Is(runErr, &fault.Error{Code: fault.CodeStreamProtocol}) {
		t.Fatalf("run error: %v", runErr)
	}
	assertEventKinds(t, events, protocol.EventTurnStarted, protocol.EventTurnFailed)
}

func TestRunTurnRejectsConcurrentTurn(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	conversation := fakeConversation{stream: func(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		close(started)
		go func() {
			<-release
			stream <- provider.StreamEvent{Kind: provider.StreamEventCompleted}
			close(stream)
		}()
		return stream, nil
	}}
	runtime := New(conversation)
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- runtime.RunTurn(context.Background(), provider.TurnInput{Text: "first"}, func(protocol.Event) {})
	}()
	<-started

	secondErr := runtime.RunTurn(context.Background(), provider.TurnInput{Text: "second"}, func(protocol.Event) {})
	if !errors.Is(secondErr, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("second turn error: %v", secondErr)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first turn: %v", err)
	}
}

func TestRunTurnCancelCompletionRacePublishesOneTerminal(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		ctx, cancel := context.WithCancel(context.Background())
		conversation := fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
			stream := make(chan provider.StreamEvent, 1)
			go func() {
				select {
				case <-ctx.Done():
					stream <- provider.StreamEvent{Kind: provider.StreamEventCancelled, Err: fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)}
				default:
					stream <- provider.StreamEvent{Kind: provider.StreamEventCompleted}
				}
				close(stream)
			}()
			return stream, nil
		}}
		runtime := New(conversation)
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
			t.Fatalf("iteration %d terminal count: %d", iteration, terminalCount.Load())
		}
	}
}

func TestChatSessionInterruptAndShutdownLifecycle(t *testing.T) {
	conversation := fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		stream := make(chan provider.StreamEvent, 1)
		go func() {
			<-ctx.Done()
			stream <- provider.StreamEvent{Kind: provider.StreamEventCancelled, Err: fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)}
			close(stream)
		}()
		return stream, nil
	}}
	session := NewChatSession(New(conversation))
	events, err := session.Submit("hello")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if event := <-events; event.Kind != protocol.EventTurnStarted {
		t.Fatalf("first event: %s", event.Kind)
	}
	session.Interrupt()
	session.Interrupt()
	var terminal protocol.Event
	for event := range events {
		terminal = event
	}
	if terminal.Kind != protocol.EventTurnFailed {
		t.Fatalf("terminal: %#v", terminal)
	}
	payload, err := protocol.DecodeTurnFailed(terminal)
	if err != nil || !payload.Cancelled {
		t.Fatalf("cancel payload: %#v err=%v", payload, err)
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Shutdown(shutdownContext); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if _, err := session.Submit("after close"); !errors.Is(err, &fault.Error{Code: fault.CodeTurnFailed}) {
		t.Fatalf("submit after close: %v", err)
	}
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

func collectTurn(runtime *Runtime, ctx context.Context) ([]protocol.Event, error) {
	var events []protocol.Event
	err := runtime.RunTurn(ctx, provider.TurnInput{Text: "hello"}, func(event protocol.Event) {
		events = append(events, event)
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

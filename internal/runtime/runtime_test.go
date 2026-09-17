package runtime

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

type fakeProvider struct{}

func (fakeProvider) Family() domain.ProviderFamily {
	return domain.ProviderAnthropic
}

func (fakeProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (fakeProvider) Stream(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
	stream := make(chan provider.StreamEvent, 1)
	stream <- provider.StreamEvent{Event: protocol.NewEvent(protocol.EventAssistantTextDelta)}
	close(stream)
	return stream, nil
}

func TestRunTurnEmitsLifecycleInOrder(t *testing.T) {
	runtime := New(fakeProvider{})
	var kinds []protocol.EventKind

	err := runtime.RunTurn(context.Background(), provider.TurnInput{Text: "hello"}, func(event protocol.Event) {
		kinds = append(kinds, event.Kind)
	})
	if err != nil {
		t.Fatalf("run turn: %v", err)
	}

	want := []protocol.EventKind{
		protocol.EventTurnStarted,
		protocol.EventAssistantTextDelta,
		protocol.EventTurnCompleted,
	}
	if len(kinds) != len(want) {
		t.Fatalf("event count: got %d want %d", len(kinds), len(want))
	}
	for index := range want {
		if kinds[index] != want[index] {
			t.Fatalf("event %d: got %s want %s", index, kinds[index], want[index])
		}
	}
}

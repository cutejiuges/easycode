package runtime

import (
	"context"
	"testing"

	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
)

func TestHostDetachDrainsFullOutputBuffer(t *testing.T) {
	produced := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan struct{})
	conversation := &fakeConversation{stream: func(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
		output := make(chan provider.StreamEvent)
		go func() {
			defer close(output)
			defer close(produced)
			<-release
			for index := range 1000 {
				semantic, _ := protocol.NewAssistantTextDelta("buffered")
				event, err := provider.NewSemanticStreamEvent(semantic)
				if err != nil {
					return
				}
				select {
				case output <- event:
					if index == 0 {
						close(delivered)
					}
				case <-ctx.Done():
					return
				}
			}
			<-ctx.Done()
		}()
		return output, nil
	}}
	journal := &fakeJournal{}
	host := NewChatSession(newTestRuntime(t, conversation, journal))
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	stream, err := host.Submit(context.Background(), "hello")
	requireNoError(t, err)
	if event := <-stream; event.Kind != protocol.EventTurnStarted {
		t.Fatal(event.Kind)
	}
	// 在 Provider 仍受 barrier 控制时填满宿主 buffer，确定性模拟已退出消费者。
	host.mu.Lock()
	var destination chan protocol.Event
	for _, value := range host.streams {
		destination = value
	}
	host.mu.Unlock()
	for range cap(destination) {
		destination <- protocol.Event{Kind: protocol.EventAssistantTextDelta}
	}
	close(release)
	<-delivered
	// 消费者退出后关闭必须排空内部流，不能等待宿主再次读取。
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-produced
	<-host.Done()
	terminals := 0
	for _, batch := range journal.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventTurnFailed {
				terminals++
			}
		}
	}
	if terminals != 1 {
		t.Fatalf("terminal count=%d", terminals)
	}
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

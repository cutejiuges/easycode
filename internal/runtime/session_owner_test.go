package runtime

import (
	"context"
	"testing"

	"easycode/internal/domain"
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
	if err != nil {
		t.Fatal(err)
	}
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

func TestHostStartCancelledCreatesNoOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	host := NewChatSession(newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{}))
	if err := host.Start(ctx); err == nil {
		t.Fatal("cancelled start succeeded")
	}
	if host.started {
		t.Fatal("cancelled start created owner")
	}
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHostInterruptDoesNotWaitForBlockedAdmission(t *testing.T) {
	// 未消费 requests 的 loop 确定性模拟被输出背压阻塞的 owner。
	loop, err := NewAgentLoop(newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{}), DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	loop.state.Store(agentLoopRunning)
	host := NewChatSession(loop.runtime)
	host.loop = loop
	host.activeTurnID = runtimeTurnID
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); host.runInterrupts(ctx) }()
	host.Interrupt()
	host.Interrupt()
	cancel()
	<-done
	if host.activeTurnID != runtimeTurnID {
		t.Fatal("interrupt changed active identity outside owner")
	}
}

func TestHostInterruptKeepsNewestTargetWithoutRetargetingOldRequest(t *testing.T) {
	host := NewChatSession(nil)
	host.activeTurnID = runtimeTurnID
	host.Interrupt()
	next := domain.TurnID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	host.activeTurnID = next
	host.Interrupt()
	if target := <-host.interrupts; target != next {
		t.Fatal("old pending target hid current interrupt")
	}
}

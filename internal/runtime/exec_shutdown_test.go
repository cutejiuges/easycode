package runtime

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/tool"
)

func TestExecShutdownTimeoutRetainsTurnUntilExecutorCleanup(t *testing.T) {
	journal := &fakeJournal{}
	_, invocation, executor := runtimeExecFixture(t, journal)
	exec, _ := invocation.Exec()
	ready, err := tool.NewExecReadyCall(exec.ProviderCallID(), exec.Input())
	if err != nil {
		t.Fatal(err)
	}
	conversation := &fakeToolConversation{fakeConversation: &fakeConversation{stream: fixedStream(completedProviderEventWithSample(t, newPreparedSampleWithReady(t, ready)))}}
	config := testRuntimeConfig(t, journal)
	config.ExecPreparer, config.ExecExecutor, config.InteractiveApprovals = executor, executor, true
	config.GenerateInvocationID = func() (tool.InvocationID, error) { return invocation.InvocationID(), nil }
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	runtime, err := New(conversation, config)
	if err != nil {
		t.Fatal(err)
	}
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	executor.onExecute = func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
	}
	facade := NewChatSession(runtime)
	if err := facade.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = facade.Shutdown(context.Background()) })
	events, err := facade.Submit(context.Background(), "run")
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Kind == protocol.EventCommandStarted {
			break
		}
		if event.Kind != protocol.EventToolApprovalRequested {
			continue
		}
		payload, err := protocol.DecodeToolApprovalRequested(event)
		if err != nil {
			t.Fatal(err)
		}
		if err := facade.ResolveApproval(context.Background(), event.TurnID, payload, protocol.ApprovalAllowOnce, ""); err != nil {
			t.Fatal(err)
		}
	}
	<-entered
	facade.Interrupt()
	<-cancelled
	if _, err := facade.Submit(context.Background(), "too early"); err == nil {
		t.Fatal("next turn admitted before command cleanup")
	}
	deadline, cancel := context.WithCancel(context.Background())
	cancel()
	if err := facade.Shutdown(deadline); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown deadline = %v", err)
	}
	facade.mu.Lock()
	ownedDone, active := facade.done, facade.started
	facade.mu.Unlock()
	if !active || ownedDone == nil {
		t.Fatal("timed-out shutdown abandoned command owner")
	}
	select {
	case <-ownedDone:
		t.Fatal("cleanup completion published too early")
	default:
	}
	close(release)
	if err := facade.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-ownedDone
	for range events {
	}
	if executor.calls != 1 {
		t.Fatalf("executor calls = %d", executor.calls)
	}
}

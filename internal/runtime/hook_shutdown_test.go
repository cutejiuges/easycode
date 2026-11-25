package runtime

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/tool"
)

func TestHookShutdownRetainsTurnUntilRunnerCleanup(t *testing.T) {
	journal := &fakeJournal{}
	_, inv, runner, ds := hookRuntimeFixture(t, journal, 1)
	patch, _ := inv.Patch()
	ready, _ := tool.NewPatchReadyCall(patch.ProviderCallID(), tool.WireCustomFreeform, patch.Input())
	conversation := &fakeToolConversation{fakeConversation: &fakeConversation{stream: fixedStream(completedProviderEventWithSample(t, newPreparedSampleWithReady(t, ready)))}}
	config := testRuntimeConfig(t, journal)
	config.InteractiveApprovals = true
	config.GenerateInvocationID = func() (tool.InvocationID, error) { return inv.InvocationID(), nil }
	config.GenerateTurnID = func() (domain.TurnID, error) { return runtimeTurnID, nil }
	r, err := New(conversation, config)
	requireNoError(t, err)
	installRuntimeTestHooks(t, r, runner, "/workspace", ds)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	runner.onRun = func(ctx context.Context, id domain.HookRunIdentity) domain.HookRunResult {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return domain.HookRunResult{Identity: id, Status: domain.HookCancel, Code: domain.HookCancelled, Cancelled: true, CleanupConfirmed: true, Origin: "execution"}
	}
	facade := NewChatSession(r)
	if err := facade.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := facade.Submit(context.Background(), "run")
	requireNoError(t, err)
	reviewed := false
	for e := range events {
		if e.Kind == protocol.EventHookTrustRequested {
			reviewed = true
			p, _ := protocol.DecodeHookTrustRequested(e)
			if err := facade.ResolveHookTrust(context.Background(), e.TurnID, p, protocol.HookTrustSession); err != nil {
				t.Fatal(err)
			}
		}
		if e.Kind == protocol.EventHookActivity && reviewed {
			break
		}
		if e.Kind == protocol.EventToolStarted || e.Kind == protocol.EventToolApprovalRequested {
			t.Fatal("target admitted before hook")
		}
	}
	<-entered
	facade.Interrupt()
	<-cancelled
	if _, err := facade.Submit(context.Background(), "too early"); err == nil {
		t.Fatal("new turn crossed cleanup")
	}
	deadline, cancel := context.WithCancel(context.Background())
	cancel()
	if err := facade.Shutdown(deadline); !errors.Is(err, context.Canceled) {
		t.Fatal("lost shutdown timeout", err)
	}
	facade.mu.Lock()
	done, active := facade.done, facade.started
	facade.mu.Unlock()
	if !active || done == nil {
		t.Fatal("abandoned cleanup owner")
	}
	select {
	case <-done:
		t.Fatal("early Done")
	default:
	}
	close(release)
	if err := facade.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-done
	for range events {
	}
	if runner.runs != 1 {
		t.Fatal("hook repeated")
	}
}

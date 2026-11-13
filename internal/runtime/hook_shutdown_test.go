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
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := domain.NewHookSnapshot(ds, nil)
	if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	for e := range events {
		if e.Kind == protocol.EventHookTrustRequested {
			p, _ := protocol.DecodeHookTrustRequested(e)
			if err := facade.ResolveHookTrust(context.Background(), e.TurnID, p, protocol.HookTrustSession); err != nil {
				t.Fatal(err)
			}
		}
		if e.Kind == protocol.EventHookActivity {
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

func TestHookTrustCancellationRejectsStaleAndCrossIdentityResponses(t *testing.T) {
	r, inv, runner, ds := hookRuntimeFixture(t, &fakeJournal{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	emitted := make(chan protocol.HookTrustRequestedPayload, 1)
	done := make(chan error, 1)
	go func() {
		_, _, err := r.checkToolHooks(ctx, func(e protocol.Event) {
			if e.Kind == protocol.EventHookTrustRequested {
				p, _ := protocol.DecodeHookTrustRequested(e)
				emitted <- p
			}
		}, runtimeTurnID, inv)
		done <- err
	}()
	p := <-emitted
	response, _ := protocol.NewHookTrustResponse(r.sessionID, r.threadID, runtimeTurnID, p, protocol.HookTrustSession)
	for _, field := range []string{"session", "thread", "turn", "request", "invocation", "revision", "fingerprint"} {
		bad := response
		id := "01900000-0000-7000-8000-000000000099"
		switch field {
		case "session":
			bad.SessionID = domain.SessionID(id)
		case "thread":
			bad.ThreadID = domain.ThreadID(id)
		case "turn":
			bad.TurnID = domain.TurnID(id)
		case "request":
			bad.RequestID = id
		case "invocation":
			_, cap, _ := bad.Target.ToolCall()
			bad.Target, _ = domain.NewToolHookTarget(id, cap)
		case "revision":
			bad.Reference.Revision = domain.HookDigest([]byte("other"))
		case "fingerprint":
			bad.Reference.Fingerprint = domain.HookDigest([]byte("other"))
		}
		if err := r.ResolveHookTrust(bad); err == nil {
			t.Fatal("cross identity authorized", field)
		}
	}
	cancel()
	if err := r.ResolveHookTrust(response); err == nil {
		t.Fatal("cancelled response granted")
	}
	if err := <-done; err == nil {
		t.Fatal("cancellation lost")
	}
	if runner.runs != 0 || r.HookEntries()[0].State != "untrusted" {
		t.Fatal("cancel installed grant")
	}
	target, _ := protocol.NewHookTrustTarget(r.sessionID, r.threadID, ds[0].Reference)
	target.Reference.Revision = domain.HookDigest([]byte("changed"))
	if err := r.ResetHookTrust(target); err == nil {
		t.Fatal("reset crossed revision")
	}
}

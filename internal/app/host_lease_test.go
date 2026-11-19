package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/session"
)

type terminalGateJournal struct {
	writer           managedJournal
	entered, release chan struct{}
}

func (journal *terminalGateJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	for _, draft := range drafts {
		if draft.EventKind() == session.EventTurnFailed {
			close(journal.entered)
			<-journal.release
		}
	}
	return journal.writer.AppendBatch(ctx, drafts)
}
func (journal *terminalGateJournal) Poisoned() bool { return journal.writer.Poisoned() }
func (journal *terminalGateJournal) Close(ctx context.Context) error {
	return journal.writer.Close(ctx)
}

func TestHostShutdownRetainsRealLeaseUntilTerminalCleanup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: "https://example.invalid", APIKey: secret.New("test-key"), Model: "gpt-test"}}
	resources, err := openChatResources(context.Background(), cfg, root, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gate := &terminalGateJournal{writer: resources.writer, entered: make(chan struct{}), release: make(chan struct{})}
	resources.writer = gate
	host := chatRuntime.NewChatSession(mustIdleAppRuntime(t, gate))
	resources.session = host
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- resources.close(ctx) }()
	<-gate.entered
	probeHostLease(t, root, resources.identity.ThreadID, "busy")
	select {
	case <-host.Done():
		t.Fatal("owner completed before terminal Sync")
	default:
	}
	close(gate.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost shutdown timeout: %v", err)
	}
	probeHostLease(t, root, resources.identity.ThreadID, "free")
	if err := resources.close(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("repeat close lost final result: %v", err)
	}
}

type finalWriterCloseGate struct {
	managedJournal
	entered, waiting, release chan struct{}
}

func (gate *finalWriterCloseGate) Close(ctx context.Context) error {
	if ctx.Done() != nil {
		close(gate.entered)
		<-ctx.Done()
		return context.DeadlineExceeded
	}
	close(gate.waiting)
	<-gate.release
	return gate.managedJournal.Close(ctx)
}

func TestTitleHostCloseWaitsForWriterAfterDeadline(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: "https://example.invalid", APIKey: secret.New("test-key"), Model: "gpt-test"}}
	resources, err := openChatResources(context.Background(), cfg, root, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := resources.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	confirmTitle(t, ctx, resources.session, "title-before-first-turn", "Named before first turn")
	gate := &finalWriterCloseGate{managedJournal: resources.writer, entered: make(chan struct{}), waiting: make(chan struct{}), release: make(chan struct{})}
	resources.writer = gate
	defer func() {
		select {
		case <-gate.release:
		default:
			close(gate.release)
		}
		_ = resources.close(context.Background())
	}()
	closeCtx, triggerDeadline := context.WithCancel(context.Background())
	defer triggerDeadline()
	done := make(chan error, 1)
	go func() { done <- resources.close(closeCtx) }()
	<-gate.entered
	triggerDeadline()
	select {
	case <-gate.waiting:
	case err := <-done:
		t.Fatalf("host returned before final writer close: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	probeHostLease(t, root, resources.identity.ThreadID, "busy")
	select {
	case <-resources.closeDone:
		t.Fatal("released dependencies before final writer close")
	default:
	}
	close(gate.release)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost writer deadline: %v", err)
	}
	probeHostLease(t, root, resources.identity.ThreadID, "free")
	_, plan := loadToolLoopPlan(t, root, resources.identity.ThreadID)
	if plan.Title != "Named before first turn" || len(plan.Turns) != 0 {
		t.Fatal("closing writer lost the title-only session")
	}
}

func probeHostLease(t *testing.T, root string, thread domain.ThreadID, expected string) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestHostLeaseProbe$")
	command.Env = append(os.Environ(), "EASYCODE_HOST_LEASE_ROOT="+root, "EASYCODE_HOST_LEASE_THREAD="+string(thread), "EASYCODE_HOST_LEASE_EXPECT="+expected)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("lease probe: %v\n%s", err, output)
	}
}

func TestHostLeaseProbe(t *testing.T) {
	root := os.Getenv("EASYCODE_HOST_LEASE_ROOT")
	if root == "" {
		return
	}
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	thread, err := domain.ParseThreadID(os.Getenv("EASYCODE_HOST_LEASE_THREAD"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repository.Open(context.Background(), thread)
	if os.Getenv("EASYCODE_HOST_LEASE_EXPECT") == "busy" {
		if !session.IsJournalBusy(err) {
			if lease != nil {
				_ = lease.Close()
			}
			t.Fatalf("expected busy: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := session.NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Records) == 0 {
		t.Fatal("journal empty after cleanup")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

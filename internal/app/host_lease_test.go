package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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

func TestTitleHostCloseWaitsForWriterAfterDeadline(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: "https://example.invalid", APIKey: secret.New("test-key"), Model: "gpt-test"}}
	resources, err := openChatResources(context.Background(), cfg, root, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := resources.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	confirmTitle(t, ctx, resources.session, "title-before-first-turn", "Named before first turn")
	final := &finalWriterWaitGate{
		writer: resources.writer.(*session.JournalWriter),
		first:  make(chan struct{}),
		retry:  make(chan struct{}),
		unlock: make(chan struct{}),
	}
	resources.writer = final
	closeCtx := writerDeadlineContext{Context: context.Background(), expired: make(chan struct{})}
	expire := sync.OnceFunc(func() { close(closeCtx.expired) })
	release := sync.OnceFunc(func() { close(final.unlock) })
	defer release()
	defer expire()
	done := make(chan error, 1)
	go func() { done <- resources.close(closeCtx) }()
	select {
	case <-final.first:
	case err := <-done:
		t.Fatalf("host skipped first structured close wait: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	expire()
	select {
	case <-final.retry:
	case err := <-done:
		t.Fatalf("host skipped final wait after deadline: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	probeHostLease(t, root, resources.identity.ThreadID, "busy")
	select {
	case <-resources.closeDone:
		t.Fatal("host published close before final wait")
	default:
	}

	release()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close did not retain the first deadline: %v", err)
	}
	probeHostLease(t, root, resources.identity.ThreadID, "free")
	if err := resources.close(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("repeat close lost the first deadline: %v", err)
	}
	_, plan := loadToolLoopPlan(t, root, resources.identity.ThreadID)
	if plan.Title != "Named before first turn" || len(plan.Turns) != 0 {
		t.Fatal("closing writer lost the title-only session")
	}
}

// finalWriterWaitGate 将宿主首次等待和超时后的实际关闭分开，继续持有真实 writer。
type finalWriterWaitGate struct {
	writer               *session.JournalWriter
	first, retry, unlock chan struct{}
}

func (gate *finalWriterWaitGate) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	return gate.writer.AppendBatch(ctx, drafts)
}

func (gate *finalWriterWaitGate) Poisoned() bool { return gate.writer.Poisoned() }

func (gate *finalWriterWaitGate) Close(ctx context.Context) error { return gate.writer.Close(ctx) }

func (gate *finalWriterWaitGate) CloseResult(ctx context.Context) (session.JournalCloseResult, error) {
	if ctx.Done() != nil {
		close(gate.first)
		<-ctx.Done()
		return session.JournalCloseResult{}, ctx.Err()
	}
	close(gate.retry)
	<-gate.unlock
	return gate.writer.CloseResult(ctx)
}

type writerDeadlineContext struct {
	context.Context
	expired chan struct{}
}

func (ctx writerDeadlineContext) Done() <-chan struct{} { return ctx.expired }

func (ctx writerDeadlineContext) Err() error {
	select {
	case <-ctx.expired:
		return context.DeadlineExceeded
	default:
		return nil
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

package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
	probe := func(expected string) {
		command := exec.Command(os.Args[0], "-test.run=^TestHostLeaseProbe$")
		command.Env = append(os.Environ(), "EASYCODE_HOST_LEASE_ROOT="+root, "EASYCODE_HOST_LEASE_THREAD="+string(resources.identity.ThreadID), "EASYCODE_HOST_LEASE_EXPECT="+expected)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("lease probe: %v\n%s", err, output)
		}
	}
	probe("busy")
	select {
	case <-host.Done():
		t.Fatal("owner completed before terminal Sync")
	default:
	}
	close(gate.release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost shutdown timeout: %v", err)
	}
	probe("free")
	if err := resources.close(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("repeat close lost final result: %v", err)
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

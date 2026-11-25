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
	"easycode/internal/secret"
)

type blockingFileBoundary struct {
	workspaceFileBoundary
	entered, cancelled, release chan struct{}
}

func (b *blockingFileBoundary) ReadWorkspaceFile(ctx context.Context, _ string, _ int) (string, int, error) {
	close(b.entered)
	<-ctx.Done()
	close(b.cancelled)
	<-b.release
	return "", 0, ctx.Err()
}

func TestFilePreparationShutdownKeepsWorkspaceWriterAndRealLeaseUntilDone(t *testing.T) {
	root := filepath.Join(privateAppTempDir(t), "sessions")
	workspace := privateAppTempDir(t)
	writeAppTestFile(t, filepath.Join(workspace, "file"), []byte("inside"), 0600)
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: "https://example.invalid", Model: "fixture-model", APIKey: secret.New("fixture-key")}}
	r := mustOpenChatResources(t, context.Background(), cfg, root, "", workspace)
	b := &blockingFileBoundary{workspaceFileBoundary: r.workspace, entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	r.fileSource.workspace = b
	draft, _ := domain.NewInteractiveInputDraft("inspect @file")
	command, _ := protocol.NewSubmitConversationalInputDraftCommand("prepare-before-shutdown", draft)
	prepared := make(chan error, 1)
	go func() { _, err := r.runtime.PrepareInput(context.Background(), command); prepared <- err }()
	<-b.entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closed := make(chan error, 1)
	go func() { closed <- r.close(ctx) }()
	<-b.cancelled
	select {
	case <-closed:
		t.Fatal("shutdown relinquished ownership before read stack exited")
	default:
	}
	select {
	case <-r.fileSource.Done():
		t.Fatal("source pretended completion")
	default:
	}
	if text, _, err := r.workspace.ReadWorkspaceFile(context.Background(), "file", domain.MaxFileSourceBytes); err != nil || text != "inside" {
		t.Fatal("workspace closed before preparation completed", err)
	}
	probe := func(expect string) {
		t.Helper()
		p := exec.Command(os.Args[0], "-test.run=^TestHostLeaseProbe$")
		p.Env = append(os.Environ(), "EASYCODE_HOST_LEASE_ROOT="+root, "EASYCODE_HOST_LEASE_THREAD="+string(r.identity.ThreadID), "EASYCODE_HOST_LEASE_EXPECT="+expect)
		if output, err := p.CombinedOutput(); err != nil {
			t.Fatalf("lease probe: %v %s", err, output)
		}
	}
	probe("busy")
	close(b.release)
	if err := <-prepared; !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled preparation reached admission", err)
	}
	if err := <-closed; !errors.Is(err, context.Canceled) {
		t.Fatal("close lost initial cancellation", err)
	}
	select {
	case <-r.fileSource.Done():
	default:
		t.Fatal("source completion not published")
	}
	probe("free")
	if _, _, err := r.workspace.ReadWorkspaceFile(context.Background(), "file", domain.MaxFileSourceBytes); err == nil {
		t.Fatal("workspace remained open after final cleanup")
	}
	if err := r.close(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal("repeated close result changed", err)
	}
}

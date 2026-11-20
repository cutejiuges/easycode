package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/session/artifact"
	"easycode/internal/tool"
	"easycode/internal/tui"
)

func TestCancelledCandidateRetainsTargetArtifactMaintenanceAndSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cwd, err := session.NormalizeCreationCWD(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	paths := dataPaths{sessionRoot: filepath.Join(t.TempDir(), "sessions")}
	cfg := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: "https://example.invalid", APIKey: secret.New("test-key"), Model: "gpt-test",
	}}
	environment := &chatEnvironment{}
	t.Cleanup(func() { _ = environment.close() })
	source, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, cwd, chatResourceHooks{
		interactiveApprovals: true, environment: environment,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.close(context.Background()) })
	if _, err := assembleChatModel(ctx, source); err != nil {
		t.Fatal(err)
	}
	sourcePath, err := source.service.repository.JournalPath(source.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	sourceArtifact := filepath.Join(addRecoveryOrphan(t, source.artifactStore, paths.sessionRoot, source.identity.ThreadID), "reservation")
	sourceArtifactBytes, err := os.ReadFile(sourceArtifact)
	if err != nil {
		t.Fatal(err)
	}

	target, err := source.service.create(ctx, testSessionProvider(&fakeProviderFactory{family: domain.ProviderOpenAI}, "responses"), "gpt-test", cwd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.writer.Close(context.Background()) })
	targetStore, err := artifact.Open(ctx, source.artifactLayout, target.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = targetStore.Close() })
	targetOrphan := addRecoveryOrphan(t, targetStore, paths.sessionRoot, target.identity.ThreadID)
	if err := targetStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := target.writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	targetPath, err := source.service.repository.JournalPath(target.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	targetBytes, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}

	owner := newPickerOwner(newPickerExecutor(cfg, paths, cwd, nil, environment))
	owner.initial = source
	owner.Start(ctx)
	t.Cleanup(func() {
		owner.Stop()
		if err := owner.Wait(); err != nil {
			t.Errorf("picker cleanup: %v", err)
		}
	})
	request := tui.PickerRequest{ID: 1, Source: source.identity.ThreadID, ThreadID: target.identity.ThreadID, Operation: tui.PickerOpen}
	if !owner.Submit(request) {
		t.Fatal("resume operation was not dispatched")
	}
	if prepared := nextRecoveryPickerResult(t, ctx, owner); prepared.Stage != tui.PickerPrepared || prepared.Chat == nil {
		t.Fatalf("candidate did not reach an accepted-ready offer: %+v", prepared)
	}
	owner.CancelResume(request)
	if result := nextRecoveryPickerResult(t, ctx, owner); result.Stage != tui.PickerCancelled || !strings.Contains(result.Error, "recovery changes were retained") {
		t.Fatalf("cancelled candidate result = %+v", result)
	}
	assertRecoveryFileBytes(t, targetPath, targetBytes)
	assertRecoveryFileBytes(t, sourcePath, sourceBytes)
	assertRecoveryFileBytes(t, sourceArtifact, sourceArtifactBytes)
	if _, err := os.Stat(targetOrphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target orphan restored after cancellation: %v", err)
	}
	assertSessionLeaseAvailable(t, source.service, target.identity.ThreadID)
	if !source.session.Live() {
		t.Fatal("source control did not survive candidate cancellation")
	}
	permit, err := source.session.ReserveResume(ctx)
	if err != nil {
		t.Fatalf("source intake stayed frozen: %v", err)
	}
	if err := permit.Release(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestInChatResumeOwnerHandlesControllerExitBeforeAccept(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	paths, cwd, cfg := pickerFixture(t)
	target := newPickerFixtureThread(t, ctx, paths, cwd)
	environment := &chatEnvironment{}
	defer environment.close()
	source, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, cwd, chatResourceHooks{
		interactiveApprovals: true, environment: environment,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.close(context.Background())
	if _, err := assembleChatModel(ctx, source); err != nil {
		t.Fatal(err)
	}

	execute := newPickerExecutor(cfg, paths, cwd, nil, environment)
	prepared := make(chan *chatResources, 1)
	owner := newPickerOwner(func(operation, lifetime context.Context, request tui.PickerRequest) pickerExecution {
		execution := execute(operation, lifetime, request)
		prepared <- execution.resources
		return execution
	})
	owner.initial = source
	owner.Start(ctx)
	defer func() { owner.Stop(); _ = owner.Wait() }()
	request := tui.PickerRequest{ID: 1, Operation: tui.PickerOpen, Source: source.identity.ThreadID, ThreadID: target}

	submitPickerRequest(t, owner, request)
	awaitPickerStage(t, ctx, owner, tui.PickerPrepared)
	first := <-prepared
	if err := first.session.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if owner.Accept(request.ID) {
		t.Fatal("accepted a closed candidate")
	}
	returned := awaitPickerStage(t, ctx, owner, tui.PickerCancelled)
	if !strings.Contains(returned.Error, "Target session ended before handoff") || !source.session.Live() {
		t.Fatalf("candidate exit did not return to the healthy source: %q", returned.Error)
	}
	assertSessionLeaseAvailable(t, source.service, target)
	requirePickerLeaseBusy(t, ctx, source.service.repository, source.identity.ThreadID)

	request.ID++
	submitPickerRequest(t, owner, request)
	awaitPickerStage(t, ctx, owner, tui.PickerPrepared)
	second := <-prepared
	if err := source.session.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if owner.Accept(request.ID) {
		t.Fatal("accepted a candidate after its source closed")
	}
	select {
	case result, open := <-owner.Results():
		if open && !result.Fatal {
			t.Fatalf("source exit sent a non-fatal picker result: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := owner.Wait(); err == nil || !strings.Contains(err.Error(), "active session ended") {
		t.Fatalf("source exit did not fail the owner: %v", err)
	}
	select {
	case <-second.session.Done():
	default:
		t.Fatal("owner exited before closing the unaccepted candidate")
	}
	assertSessionLeaseAvailable(t, source.service, source.identity.ThreadID)
	assertSessionLeaseAvailable(t, source.service, target)
}

func TestCandidateCleanupDistinguishesPriorPoisonFromFinalFailure(t *testing.T) {
	syncErr := errors.New("fixture final sync failed")
	closeErr := errors.New("fixture descriptor close failed")
	for _, tc := range []struct {
		name    string
		outcome session.JournalCloseResult
		wantErr error
	}{
		{name: "previous uncertainty only", outcome: session.JournalCloseResult{Poisoned: true}},
		{name: "final sync failed", outcome: session.JournalCloseResult{Poisoned: true, SyncErr: syncErr}, wantErr: syncErr},
		{name: "descriptor close failed", outcome: session.JournalCloseResult{Poisoned: true, CloseErr: closeErr}, wantErr: closeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempt := &recoveryAttempt{committed: true, uncertain: true}
			err := attempt.closeJournal(&candidateCloseResult{result: tc.outcome})
			if !errors.Is(err, tc.wantErr) || !errors.Is(attempt.cleanupErr, tc.wantErr) {
				t.Fatalf("candidate close = %v, recorded cleanup = %v", err, attempt.cleanupErr)
			}
			if !attempt.committed || !attempt.uncertain || !attempt.fatal() {
				t.Fatal("final closure erased the earlier durable history or startup decision")
			}
		})
	}
}

type candidateCloseResult struct {
	managedJournal
	result session.JournalCloseResult
}

func (journal *candidateCloseResult) CloseResult(context.Context) (session.JournalCloseResult, error) {
	return journal.result, nil
}

func addRecoveryOrphan(t *testing.T, store *artifact.Store, root string, thread domain.ThreadID) string {
	t.Helper()
	invocation, err := tool.GenerateInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	capture, err := store.Start(context.Background(), invocation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capture.WriteStdout([]byte("fixture orphan")); err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "artifacts", string(thread), string(invocation))
}

func nextRecoveryPickerResult(t *testing.T, ctx context.Context, owner *pickerOwner) tui.PickerResult {
	t.Helper()
	select {
	case result, ok := <-owner.Results():
		if ok {
			return result
		}
		t.Fatalf("picker finished before delivering result: %v", owner.Wait())
	case <-ctx.Done():
		t.Fatalf("waiting for picker result: %v", ctx.Err())
	}
	return tui.PickerResult{}
}

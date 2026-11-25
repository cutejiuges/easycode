package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/session"
)

type fakeProviderFactory struct {
	family       domain.ProviderFamily
	restoreErr   error
	newCalls     atomic.Int32
	restoreCalls atomic.Int32
	networkCalls atomic.Int32
	restored     []provider.NativeCommitEnvelope
}

func testSessionProvider(factory provider.Factory, wire string) sessionProvider {
	binding, _ := bindProvider(factory.Family())
	binding.wire = wire
	return binding.bindSession(factory)
}

func (factory *fakeProviderFactory) Family() domain.ProviderFamily { return factory.family }

func (*fakeProviderFactory) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (factory *fakeProviderFactory) NewConversation(threadID domain.ThreadID) (provider.Conversation, error) {
	factory.newCalls.Add(1)
	return &fakeAppConversation{family: factory.family, networkCalls: &factory.networkCalls}, nil
}

func (factory *fakeProviderFactory) RestoreConversation(_ context.Context, threadID domain.ThreadID, commits []provider.RestoreRecord) (provider.Conversation, provider.CheckpointReader, error) {
	factory.restoreCalls.Add(1)
	if factory.restoreErr != nil {
		return nil, nil, factory.restoreErr
	}
	factory.restored = make([]provider.NativeCommitEnvelope, len(commits))
	for index, commit := range commits {
		cloned, err := commit.Envelope()
		if err != nil {
			return nil, nil, err
		}
		factory.restored[index] = cloned
	}
	return &fakeAppConversation{family: factory.family, networkCalls: &factory.networkCalls}, nil, nil
}

type fakeAppConversation struct {
	family       domain.ProviderFamily
	networkCalls *atomic.Int32
	history      domain.SemanticHistoryView
}

func (conversation *fakeAppConversation) Family() domain.ProviderFamily { return conversation.family }

func (*fakeAppConversation) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (conversation *fakeAppConversation) ProjectHistory() domain.SemanticHistoryView {
	turns := append([]domain.SemanticTurn(nil), conversation.history.Turns...)
	return domain.SemanticHistoryView{Provider: conversation.family, Turns: turns}
}

func (conversation *fakeAppConversation) historyFootprint() (domain.NativeHistoryFootprint, error) {
	estimated, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, 0)
	return domain.NewNativeHistoryFootprint(conversation.family, 0, estimated)
}

func (conversation *fakeAppConversation) Stream(context.Context, provider.TurnInput) (provider.StreamEvents, error) {
	conversation.networkCalls.Add(1)
	return nil, errors.New("unexpected network call")
}

func TestSessionServiceOwnershipFailuresReleaseTheCurrentOwner(t *testing.T) {
	t.Run("replay planner", func(t *testing.T) {
		service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
		defer service.close()
		service.plan = func(context.Context, session.LoadResult) (session.ReplayPlan, error) {
			return session.ReplayPlan{}, errors.New("fixture replay failure")
		}
		factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
		if _, err := service.resume(context.Background(), testSessionProvider(factory, "responses"), "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionCorruption}) {
			t.Fatalf("resume error = %v", err)
		}
		if factory.restoreCalls.Load() != 0 {
			t.Fatalf("restore calls = %d", factory.restoreCalls.Load())
		}
		assertSessionLeaseAvailable(t, service, identity.ThreadID)
	})

	t.Run("mismatched thread", func(t *testing.T) {
		service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
		defer service.close()
		differentThread, err := domain.GenerateThreadID()
		if err != nil {
			t.Fatal(err)
		}
		planner := session.NewReplayPlanner()
		service.plan = func(ctx context.Context, loaded session.LoadResult) (session.ReplayPlan, error) {
			plan, err := planner.PlanContext(ctx, loaded)
			plan.Identity.ThreadID = differentThread
			return plan, err
		}
		factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
		if _, err := service.resume(context.Background(), testSessionProvider(factory, "responses"), "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionIncompatible}) {
			t.Fatalf("resume error = %v", err)
		}
		if factory.restoreCalls.Load() != 0 {
			t.Fatalf("restore calls = %d", factory.restoreCalls.Load())
		}
		assertSessionLeaseAvailable(t, service, identity.ThreadID)
	})

	t.Run("mismatched wire", func(t *testing.T) {
		service, identity := createClosedTestSession(t, domain.ProviderOpenAI, domain.AnthropicMessagesWire, "gpt-test")
		defer service.close()
		factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
		binding, err := bindProvider(factory.Family())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = service.resume(context.Background(), binding.bindSession(factory), "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionIncompatible}) {
			t.Fatalf("resume error = %v", err)
		}
		if preview, cleanupFailed, err := previewSession(context.Background(), service.repository.RootPath(), identity.ThreadID, defaultPreviewDependencies()); err == nil || err.Error() != "stored history source is unsupported" || cleanupFailed || len(preview.Messages) != 0 {
			t.Fatalf("preview mismatch = %#v, %t, %v", preview, cleanupFailed, err)
		}
		if factory.restoreCalls.Load() != 0 || factory.networkCalls.Load() != 0 {
			t.Fatal("mismatched wire entered a provider")
		}
		assertSessionLeaseAvailable(t, service, identity.ThreadID)
	})

	t.Run("provider restore", func(t *testing.T) {
		service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
		defer service.close()
		factory := &fakeProviderFactory{
			family: domain.ProviderOpenAI, restoreErr: errors.New("fixture restore failure"),
		}
		if _, err := service.resume(context.Background(), testSessionProvider(factory, "responses"), "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionCorruption}) {
			t.Fatalf("resume error = %v", err)
		}
		if factory.restoreCalls.Load() != 1 || factory.networkCalls.Load() != 0 {
			t.Fatalf("provider calls restore/network = %d/%d", factory.restoreCalls.Load(), factory.networkCalls.Load())
		}
		assertSessionLeaseAvailable(t, service, identity.ThreadID)
	})

	t.Run("writer start", func(t *testing.T) {
		service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
		defer service.close()
		service.start = func(*session.JournalLease, session.Identity, uint64) (journalStartResult, error) {
			return journalStartResult{}, errors.New("fixture writer start failure")
		}
		factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
		if _, err := service.resume(context.Background(), testSessionProvider(factory, "responses"), "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
			t.Fatalf("resume error = %v", err)
		}
		if factory.restoreCalls.Load() != 1 || factory.networkCalls.Load() != 0 {
			t.Fatalf("provider calls restore/network = %d/%d", factory.restoreCalls.Load(), factory.networkCalls.Load())
		}
		assertSessionLeaseAvailable(t, service, identity.ThreadID)
	})
}

func TestSessionServiceInChatRevalidatesBeforeRepair(t *testing.T) {
	service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
	defer service.close()
	path, durable, torn := installTornSessionTail(t, service, identity)
	_, plan := loadAppPlan(t, service, identity.ThreadID)
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}

	service.expectedResumeCWD = t.TempDir()
	if _, err := service.resume(context.Background(), testSessionProvider(factory, "responses"), "gpt-test", identity.ThreadID); !errors.Is(err, errResumeDirectory) {
		t.Fatalf("wrong directory = %v", err)
	}
	assertSessionLeaseAvailable(t, service, identity.ThreadID)
	assertRecoveryFileBytes(t, path, torn)

	service.expectedResumeCWD = plan.SessionMetadata.CreationCWD
	if _, err := service.resume(context.Background(), testSessionProvider(factory, "responses"), "other-model", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionIncompatible}) || errors.Is(err, errResumeDirectory) {
		t.Fatalf("wrong model = %v", err)
	}
	assertSessionLeaseAvailable(t, service, identity.ThreadID)
	assertRecoveryFileBytes(t, path, torn)
	if factory.restoreCalls.Load() != 0 || factory.networkCalls.Load() != 0 {
		t.Fatal("incompatible recovery ran a Provider")
	}

	// 启动恢复允许使用原有跨目录入口。
	service.expectedResumeCWD = ""
	resumed, err := service.resume(context.Background(), testSessionProvider(factory, "responses"), "gpt-test", identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.repair.Repaired {
		t.Fatal("startup recovery did not repair the validated tail")
	}
	if err := resumed.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertRecoveryFileBytes(t, path, durable)
}

func TestSessionServiceRecoveryRecordsOnlyConfirmedRepair(t *testing.T) {
	service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
	defer service.close()
	path, durable, torn := installTornSessionTail(t, service, identity)
	_, plan := loadAppPlan(t, service, identity.ThreadID)
	service.expectedResumeCWD = plan.SessionMetadata.CreationCWD
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	replay, start := service.plan, service.start

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.recovery = &recoveryAttempt{}
	service.plan = func(ctx context.Context, loaded session.LoadResult) (session.ReplayPlan, error) {
		result, err := replay(ctx, loaded)
		cancel()
		return result, err
	}
	if _, err := service.resume(ctx, testSessionProvider(factory, "responses"), "gpt-test", identity.ThreadID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled before repair = %v", err)
	}
	if service.recovery.committed || service.recovery.uncertain || service.recovery.cleanupErr != nil {
		t.Fatalf("cancelled repair result = %+v", service.recovery)
	}
	assertRecoveryFileBytes(t, path, torn)
	assertSessionLeaseAvailable(t, service, identity.ThreadID)

	repairedCtx, cancelAfterRepair := context.WithCancel(context.Background())
	defer cancelAfterRepair()
	service.recovery = &recoveryAttempt{}
	service.plan = replay
	service.start = func(lease *session.JournalLease, identity session.Identity, seq uint64) (journalStartResult, error) {
		result, err := start(lease, identity, seq)
		cancelAfterRepair()
		return result, err
	}
	if _, err := service.resume(repairedCtx, testSessionProvider(factory, "responses"), "gpt-test", identity.ThreadID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled after repair = %v", err)
	}
	if !service.recovery.committed || service.recovery.uncertain || service.recovery.cleanupErr != nil {
		t.Fatalf("completed repair result = %+v", service.recovery)
	}
	assertRecoveryFileBytes(t, path, durable)
	assertSessionLeaseAvailable(t, service, identity.ThreadID)
}

func installTornSessionTail(t *testing.T, service *sessionService, identity session.Identity) (path string, durable, torn []byte) {
	t.Helper()
	path, err := service.repository.JournalPath(identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	durable, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	torn = append(bytes.Clone(durable), `{"incomplete":`...)
	writeAppTestFile(t, path, torn, 0o600)
	return path, durable, torn
}

func assertRecoveryFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	if result, err := os.ReadFile(path); err != nil || !bytes.Equal(result, want) {
		t.Fatalf("%s bytes = %d, want %d, read error = %v", path, len(result), len(want), err)
	}
}

func TestSessionServiceInterruptedCompensationFailureDoesNotBecomeUsable(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	service.recovery = &recoveryAttempt{}
	defer service.close()
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	created, err := service.create(context.Background(), testSessionProvider(factory, "responses"), "gpt-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	turnID, _ := domain.GenerateTurnID()
	startedDraft, err := session.NewTurnStartedDraft(turnID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = created.writer.AppendBatch(context.Background(), []session.RecordDraft{startedDraft})
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	failing := &failingManagedJournal{}
	service.start = func(*session.JournalLease, session.Identity, uint64) (journalStartResult, error) {
		return journalStartResult{writer: failing}, nil
	}
	restorer := &fakeProviderFactory{family: domain.ProviderOpenAI}
	resumed, err := service.resume(context.Background(), testSessionProvider(restorer, "responses"), "gpt-test", created.identity.ThreadID)
	if !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) || resumed.writer != nil {
		t.Fatalf("resume = %#v, %v", resumed, err)
	}
	if failing.appendCalls.Load() != 1 || failing.closeCalls.Load() != 1 || restorer.networkCalls.Load() != 0 {
		t.Fatalf("append/close/network = %d/%d/%d", failing.appendCalls.Load(), failing.closeCalls.Load(), restorer.networkCalls.Load())
	}
	if !service.recovery.fatal() {
		t.Fatal("uncertain durable compensation classified retryable")
	}
	assertSessionLeaseAvailable(t, service, created.identity.ThreadID)
}

func TestSessionServiceInterruptedAppendFailureReleasesLease(t *testing.T) {
	service := newTestSessionService(t)
	service.recovery = &recoveryAttempt{}
	defer service.close()
	created, err := service.create(
		context.Background(), testSessionProvider(&fakeProviderFactory{family: domain.ProviderOpenAI}, "responses"), "gpt-test", t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}
	turnID, err := domain.GenerateTurnID()
	if err != nil {
		t.Fatal(err)
	}
	startedDraft, err := session.NewTurnStartedDraft(turnID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.writer.AppendBatch(context.Background(), []session.RecordDraft{startedDraft}); err != nil {
		t.Fatal(err)
	}
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	failing := &failingManagedJournal{appendErr: errors.New("fixture append failure")}
	service.start = func(*session.JournalLease, session.Identity, uint64) (journalStartResult, error) {
		return journalStartResult{writer: failing}, nil
	}
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	if _, err := service.resume(context.Background(), testSessionProvider(factory, "responses"), "gpt-test", created.identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
		t.Fatalf("resume error = %v", err)
	}
	if failing.appendCalls.Load() != 1 || failing.closeCalls.Load() != 1 || factory.networkCalls.Load() != 0 {
		t.Fatalf("append/close/network = %d/%d/%d", failing.appendCalls.Load(), failing.closeCalls.Load(), factory.networkCalls.Load())
	}
	if !service.recovery.fatal() {
		t.Fatal("uncertain durable compensation classified retryable")
	}
	assertSessionLeaseAvailable(t, service, created.identity.ThreadID)
}

type failingManagedJournal struct {
	appendCalls atomic.Int32
	closeCalls  atomic.Int32
	appendErr   error
}

func (journal *failingManagedJournal) AppendBatch(context.Context, []session.RecordDraft) ([]session.Record, error) {
	journal.appendCalls.Add(1)
	if journal.appendErr != nil {
		return nil, journal.appendErr
	}
	return nil, errors.New("fixture sync failure")
}

func (*failingManagedJournal) Poisoned() bool { return true }

func (journal *failingManagedJournal) Close(context.Context) error {
	journal.closeCalls.Add(1)
	return nil
}

func newTestSessionService(t *testing.T) *sessionService {
	t.Helper()
	service, err := openSessionService(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func createClosedTestSession(
	t *testing.T,
	family domain.ProviderFamily,
	wire string,
	model string,
) (*sessionService, session.Identity) {
	t.Helper()
	service := newTestSessionService(t)
	created, err := service.create(
		context.Background(), testSessionProvider(&fakeProviderFactory{family: family}, wire), model, t.TempDir(),
	)
	if err != nil {
		_ = service.close()
		t.Fatal(err)
	}
	if err := created.writer.Close(context.Background()); err != nil {
		_ = service.close()
		t.Fatal(err)
	}
	return service, created.identity
}

func assertSessionLeaseAvailable(t *testing.T, service *sessionService, threadID domain.ThreadID) {
	t.Helper()
	lease, err := service.repository.Open(context.Background(), threadID)
	if err != nil {
		t.Fatalf("journal lease was not released: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func loadAppPlan(t *testing.T, service *sessionService, threadID domain.ThreadID) (session.LoadResult, session.ReplayPlan) {
	t.Helper()
	lease, err := service.repository.Open(context.Background(), threadID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := session.NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := session.NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	return loaded, plan
}

// ProjectAssistantSample 在不模拟原生采样的测试替身中明确失败，不猜测展示列表。
func (*fakeAppConversation) ProjectAssistantSample(domain.AssistantSampleReference) (domain.AssistantSampleText, error) {
	return domain.AssistantSampleText{}, domain.HookConfigInvalid
}

package app

import (
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
	"easycode/internal/tool"
)

type fakeProviderFactory struct {
	family       domain.ProviderFamily
	restoreErr   error
	newCalls     atomic.Int32
	restoreCalls atomic.Int32
	networkCalls atomic.Int32
	restored     []provider.NativeCommitEnvelope
}

func (factory *fakeProviderFactory) Family() domain.ProviderFamily { return factory.family }

func (*fakeProviderFactory) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (factory *fakeProviderFactory) NewConversation() provider.Conversation {
	factory.newCalls.Add(1)
	return &fakeAppConversation{family: factory.family, networkCalls: &factory.networkCalls}
}

func (factory *fakeProviderFactory) RestoreConversation(commits []provider.NativeCommitEnvelope) (provider.Conversation, error) {
	factory.restoreCalls.Add(1)
	if factory.restoreErr != nil {
		return nil, factory.restoreErr
	}
	factory.restored = make([]provider.NativeCommitEnvelope, len(commits))
	for index, commit := range commits {
		cloned, err := commit.Clone()
		if err != nil {
			return nil, err
		}
		factory.restored[index] = cloned
	}
	return &fakeAppConversation{family: factory.family, networkCalls: &factory.networkCalls}, nil
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

func (conversation *fakeAppConversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	estimated, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, 0)
	return domain.NewNativeHistoryFootprint(conversation.family, 0, estimated)
}

func (conversation *fakeAppConversation) Stream(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
	conversation.networkCalls.Add(1)
	return nil, errors.New("unexpected network call")
}

func TestSessionServiceCreatesPrivateRootMetadataWithoutNetwork(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	created, err := service.create(context.Background(), factory, "responses", "gpt-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, plan := loadAppPlan(t, service, created.identity.ThreadID)
	if loaded.NextSequence != 3 || plan.Identity != created.identity ||
		plan.SessionMetadata.Provider != domain.ProviderOpenAI ||
		plan.SessionMetadata.ProviderWire != "responses" || plan.SessionMetadata.Model != "gpt-test" ||
		!filepath.IsAbs(plan.SessionMetadata.CreationCWD) {
		t.Fatalf("created plan = %#v", plan)
	}
	if factory.newCalls.Load() != 1 || factory.restoreCalls.Load() != 0 || factory.networkCalls.Load() != 0 {
		t.Fatalf("provider calls new/restore/network = %d/%d/%d", factory.newCalls.Load(), factory.restoreCalls.Load(), factory.networkCalls.Load())
	}
}

func TestSessionServiceResumesCompatibleRootWithoutNetwork(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	creator := &fakeProviderFactory{family: domain.ProviderAnthropic}
	created, err := service.create(context.Background(), creator, "messages", "claude-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restorer := &fakeProviderFactory{family: domain.ProviderAnthropic}
	resumed, err := service.resume(context.Background(), restorer, "messages", "claude-test", created.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.identity != created.identity || len(restorer.restored) != 0 || restorer.restoreCalls.Load() != 1 {
		t.Fatalf("resumed = %#v restored=%d", resumed, len(restorer.restored))
	}
	if restorer.networkCalls.Load() != 0 {
		t.Fatalf("resume made network calls: %d", restorer.networkCalls.Load())
	}
	if err := resumed.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionServiceMismatchDoesNotModifyJournalOrRestoreProvider(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	creator := &fakeProviderFactory{family: domain.ProviderOpenAI}
	created, err := service.create(context.Background(), creator, "responses", "gpt-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, err := service.repository.JournalPath(created.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := []struct {
		family domain.ProviderFamily
		wire   string
		model  string
	}{
		{family: domain.ProviderAnthropic, wire: "messages", model: "gpt-test"},
		{family: domain.ProviderOpenAI, wire: "chat_completions", model: "gpt-test"},
		{family: domain.ProviderOpenAI, wire: "responses", model: "different-model"},
	}
	for _, fixture := range fixtures {
		factory := &fakeProviderFactory{family: fixture.family}
		if _, err := service.resume(context.Background(), factory, fixture.wire, fixture.model, created.identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionIncompatible}) {
			t.Fatalf("resume mismatch error = %v", err)
		}
		if factory.restoreCalls.Load() != 0 || factory.networkCalls.Load() != 0 {
			t.Fatalf("mismatch provider calls = %d/%d", factory.restoreCalls.Load(), factory.networkCalls.Load())
		}
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(after) != string(before) {
			t.Fatal("configuration mismatch modified journal")
		}
	}
}

func TestSessionServiceBusyFailsBeforeRestoreAndKeepsBytes(t *testing.T) {
	owner := newTestSessionService(t)
	defer owner.close()
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	created, err := owner.create(context.Background(), factory, "responses", "gpt-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := owner.repository.JournalPath(created.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	competitor, err := openSessionService(owner.repository.RootPath())
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.close()
	restorer := &fakeProviderFactory{family: domain.ProviderOpenAI}
	resumed, err := competitor.resume(
		context.Background(), restorer, "responses", "gpt-test", created.identity.ThreadID,
	)
	if !errors.Is(err, &fault.Error{Code: fault.CodeSessionBusy}) || resumed.writer != nil {
		t.Fatalf("busy resume = %#v, %v", resumed, err)
	}
	if restorer.restoreCalls.Load() != 0 || restorer.networkCalls.Load() != 0 {
		t.Fatalf("busy provider calls restore/network = %d/%d", restorer.restoreCalls.Load(), restorer.networkCalls.Load())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("busy resume modified journal bytes")
	}
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	resumed, err = competitor.resume(
		context.Background(), restorer, "responses", "gpt-test", created.identity.ThreadID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := resumed.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSessionServiceRejectsNewerRequiredFixtureBeforeProviderRestore(t *testing.T) {
	service := newTestSessionService(t)
	defer service.close()
	rootFixture, err := os.ReadFile(filepath.Join("..", "session", "testdata", "current", "root.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	newer, err := os.ReadFile(filepath.Join("..", "session", "testdata", "current", "newer_required.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	content := append(append([]byte(nil), rootFixture...), newer...)
	threadID := domain.ThreadID("00000000-0001-7000-8000-000000000002")
	lease, err := service.repository.Create(context.Background(), threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := service.repository.JournalPath(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	if _, err := service.resume(context.Background(), factory, "responses", "fixture-model", threadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionCorruption}) {
		t.Fatalf("newer required resume error = %v", err)
	}
	if factory.restoreCalls.Load() != 0 || factory.networkCalls.Load() != 0 {
		t.Fatalf("provider calls restore/network = %d/%d", factory.restoreCalls.Load(), factory.networkCalls.Load())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(content) {
		t.Fatal("newer required resume modified fixture bytes")
	}
	assertSessionLeaseAvailable(t, service, threadID)
}

func TestSessionServiceOwnershipFailuresReleaseTheCurrentOwner(t *testing.T) {
	t.Run("replay planner", func(t *testing.T) {
		service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
		defer service.close()
		service.plan = func(session.LoadResult) (session.ReplayPlan, error) {
			return session.ReplayPlan{}, errors.New("fixture replay failure")
		}
		factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
		if _, err := service.resume(context.Background(), factory, "responses", "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionCorruption}) {
			t.Fatalf("resume error = %v", err)
		}
		if factory.restoreCalls.Load() != 0 {
			t.Fatalf("restore calls = %d", factory.restoreCalls.Load())
		}
		assertSessionLeaseAvailable(t, service, identity.ThreadID)
	})

	t.Run("non root", func(t *testing.T) {
		service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
		defer service.close()
		planner := session.NewReplayPlanner()
		service.plan = func(loaded session.LoadResult) (session.ReplayPlan, error) {
			plan, err := planner.Plan(loaded)
			plan.ThreadMetadata.Root = false
			return plan, err
		}
		factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
		if _, err := service.resume(context.Background(), factory, "responses", "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionIncompatible}) {
			t.Fatalf("resume error = %v", err)
		}
		if factory.restoreCalls.Load() != 0 {
			t.Fatalf("restore calls = %d", factory.restoreCalls.Load())
		}
		assertSessionLeaseAvailable(t, service, identity.ThreadID)
	})

	t.Run("provider restore", func(t *testing.T) {
		service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
		defer service.close()
		factory := &fakeProviderFactory{
			family: domain.ProviderOpenAI, restoreErr: errors.New("fixture restore failure"),
		}
		if _, err := service.resume(context.Background(), factory, "responses", "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionCorruption}) {
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
		if _, err := service.resume(context.Background(), factory, "responses", "gpt-test", identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
			t.Fatalf("resume error = %v", err)
		}
		if factory.restoreCalls.Load() != 1 || factory.networkCalls.Load() != 0 {
			t.Fatalf("provider calls restore/network = %d/%d", factory.restoreCalls.Load(), factory.networkCalls.Load())
		}
		assertSessionLeaseAvailable(t, service, identity.ThreadID)
	})
}

func TestSessionServiceClosesInterruptedTailBeforeReturning(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	created, err := service.create(context.Background(), factory, "responses", "gpt-test", t.TempDir())
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
	restorer := &fakeProviderFactory{family: domain.ProviderOpenAI}
	resumed, err := service.resume(context.Background(), restorer, "responses", "gpt-test", created.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if restorer.networkCalls.Load() != 0 {
		t.Fatal("interrupted-tail compensation made a network call")
	}
	if err := resumed.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, plan := loadAppPlan(t, service, created.identity.ThreadID)
	if plan.InterruptedTail != nil || loaded.NextSequence != 5 {
		t.Fatalf("compensated plan = %#v next=%d", plan, loaded.NextSequence)
	}
	last := loaded.Records[len(loaded.Records)-1]
	failure, err := session.DecodeTurnFailedPayload(last)
	if err != nil {
		t.Fatal(err)
	}
	if last.TurnID != turnID || failure.Code != "session_interrupted" {
		t.Fatalf("compensation = %#v %#v", last, failure)
	}
}

func TestSessionServiceReturnsSealedToolRecoveryWithoutExternalCalls(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	created, err := service.create(
		context.Background(), &fakeProviderFactory{family: domain.ProviderOpenAI},
		"responses", "gpt-test", t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}
	turnID, err := domain.GenerateTurnID()
	if err != nil {
		t.Fatal(err)
	}
	started, err := session.NewTurnStartedDraft(turnID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.writer.AppendBatch(context.Background(), []session.RecordDraft{started}); err != nil {
		t.Fatal(err)
	}
	invocationID, err := tool.ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := tool.ParseProviderCallID("call-resume")
	input, _ := tool.NewReadInput("README.md", 1, 20)
	ready, _ := tool.NewReadyCall(callID, input)
	invocation, err := tool.NewReadInvocation(invocationID, ready)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := session.NewProviderNativeCommitDraft(turnID, session.NativeCommitPayload{
		Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 1,
		Payload: []byte(`{"shape":"call_sample"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(1), domain.NotApplicableUsageMetric(), domain.NotApplicableUsageMetric(),
		domain.KnownUsageMetric(1), domain.NotApplicableUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	usageDraft, err := session.NewSampleUsageDraft(turnID, usage)
	if err != nil {
		t.Fatal(err)
	}
	readyDraft, err := session.NewToolCallReadyDraft(turnID, invocation, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.writer.AppendBatch(
		context.Background(), []session.RecordDraft{commit, usageDraft, readyDraft},
	); err != nil {
		t.Fatal(err)
	}
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	resumed, err := service.resume(
		context.Background(), factory, "responses", "gpt-test", created.identity.ThreadID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.toolRecovery == nil || resumed.toolRecovery.TurnID() != turnID ||
		len(resumed.toolRecovery.Calls()) != 1 || factory.networkCalls.Load() != 0 {
		t.Fatalf("tool recovery/network = %#v/%d", resumed.toolRecovery, factory.networkCalls.Load())
	}
	if err := resumed.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, plan := loadAppPlan(t, service, created.identity.ThreadID)
	if plan.ToolRecovery == nil || plan.ToolRecovery.TurnID() != turnID {
		t.Fatalf("session service mutated recovery tail: %#v", plan.ToolRecovery)
	}
}

func TestSessionServiceInterruptedCompensationFailureDoesNotBecomeUsable(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	created, err := service.create(context.Background(), factory, "responses", "gpt-test", t.TempDir())
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
	resumed, err := service.resume(context.Background(), restorer, "responses", "gpt-test", created.identity.ThreadID)
	if !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) || resumed.writer != nil {
		t.Fatalf("resume = %#v, %v", resumed, err)
	}
	if failing.appendCalls.Load() != 1 || failing.closeCalls.Load() != 1 || restorer.networkCalls.Load() != 0 {
		t.Fatalf("append/close/network = %d/%d/%d", failing.appendCalls.Load(), failing.closeCalls.Load(), restorer.networkCalls.Load())
	}
	assertSessionLeaseAvailable(t, service, created.identity.ThreadID)
}

func TestSessionServiceInterruptedAppendFailureReleasesLease(t *testing.T) {
	service := newTestSessionService(t)
	defer service.close()
	created, err := service.create(
		context.Background(), &fakeProviderFactory{family: domain.ProviderOpenAI},
		"responses", "gpt-test", t.TempDir(),
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
	if _, err := service.resume(context.Background(), factory, "responses", "gpt-test", created.identity.ThreadID); !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
		t.Fatalf("resume error = %v", err)
	}
	if failing.appendCalls.Load() != 1 || failing.closeCalls.Load() != 1 || factory.networkCalls.Load() != 0 {
		t.Fatalf("append/close/network = %d/%d/%d", failing.appendCalls.Load(), failing.closeCalls.Load(), factory.networkCalls.Load())
	}
	assertSessionLeaseAvailable(t, service, created.identity.ThreadID)
}

func TestSessionServiceCorruptionIsNotBusyOrWriteFailure(t *testing.T) {
	service, identity := createClosedTestSession(t, domain.ProviderOpenAI, "responses", "gpt-test")
	defer service.close()
	path, err := service.repository.JournalPath(identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{invalid json}\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	_, err = service.resume(context.Background(), factory, "responses", "gpt-test", identity.ThreadID)
	if !errors.Is(err, &fault.Error{Code: fault.CodeSessionCorruption}) ||
		errors.Is(err, &fault.Error{Code: fault.CodeSessionBusy}) ||
		errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) {
		t.Fatalf("corrupt resume error = %v", err)
	}
	if factory.restoreCalls.Load() != 0 || factory.networkCalls.Load() != 0 {
		t.Fatalf("provider calls restore/network = %d/%d", factory.restoreCalls.Load(), factory.networkCalls.Load())
	}
	assertSessionLeaseAvailable(t, service, identity.ThreadID)
}

func TestSessionServiceRejectsUnknownThreadWithoutCreatingReplacement(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	unknown, err := domain.GenerateThreadID()
	if err != nil {
		t.Fatal(err)
	}
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	if _, err := service.resume(context.Background(), factory, "responses", "gpt-test", unknown); !errors.Is(err, &fault.Error{Code: fault.CodeSessionNotFound}) {
		t.Fatalf("resume unknown error = %v", err)
	}
	if factory.newCalls.Load() != 0 || factory.restoreCalls.Load() != 0 || factory.networkCalls.Load() != 0 {
		t.Fatalf("provider calls = %d/%d/%d", factory.newCalls.Load(), factory.restoreCalls.Load(), factory.networkCalls.Load())
	}
	path, err := service.repository.JournalPath(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unknown resume created replacement journal: %v", err)
	}
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
		context.Background(), &fakeProviderFactory{family: family}, wire, model, t.TempDir(),
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

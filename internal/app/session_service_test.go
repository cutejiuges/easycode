package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/session"
)

type fakeProviderFactory struct {
	family       domain.ProviderFamily
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
	factory.restored = make([]provider.NativeCommitEnvelope, len(commits))
	for index, commit := range commits {
		factory.restored[index] = commit.Clone()
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

func TestSessionServiceClosesInterruptedTailBeforeReturning(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	created, err := service.create(context.Background(), factory, "responses", "gpt-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	turnID, err := domain.NewTurnID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.writer.AppendBatch(context.Background(), []session.RecordDraft{{
		EventKind: session.EventTurnStarted, TurnID: turnID, Payload: session.TurnStartedPayload{},
	}}); err != nil {
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
	payload, err := session.DecodePayload(last)
	if err != nil {
		t.Fatal(err)
	}
	failure := payload.(session.TurnFailedPayload)
	if last.TurnID != turnID || failure.Code != "session_interrupted" {
		t.Fatalf("compensation = %#v %#v", last, failure)
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
	turnID, _ := domain.NewTurnID()
	_, _ = created.writer.AppendBatch(context.Background(), []session.RecordDraft{{
		EventKind: session.EventTurnStarted, TurnID: turnID, Payload: session.TurnStartedPayload{},
	}})
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	failing := &failingManagedJournal{}
	service.reopen = func(context.Context, *session.Repository, session.LoadResult) (managedJournal, error) {
		return failing, nil
	}
	restorer := &fakeProviderFactory{family: domain.ProviderOpenAI}
	resumed, err := service.resume(context.Background(), restorer, "responses", "gpt-test", created.identity.ThreadID)
	if !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) || resumed.writer != nil {
		t.Fatalf("resume = %#v, %v", resumed, err)
	}
	if failing.appendCalls.Load() != 1 || failing.closeCalls.Load() != 1 || restorer.networkCalls.Load() != 0 {
		t.Fatalf("append/close/network = %d/%d/%d", failing.appendCalls.Load(), failing.closeCalls.Load(), restorer.networkCalls.Load())
	}
}

func TestSessionServiceRejectsUnknownThreadWithoutCreatingReplacement(t *testing.T) {
	t.Parallel()
	service := newTestSessionService(t)
	defer service.close()
	unknown, err := domain.NewThreadID()
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
}

func (journal *failingManagedJournal) AppendBatch(context.Context, []session.RecordDraft) ([]session.Record, error) {
	journal.appendCalls.Add(1)
	return nil, errors.New("fixture sync failure")
}

func (*failingManagedJournal) Poisoned() bool { return true }

func (journal *failingManagedJournal) Close(context.Context) error {
	journal.closeCalls.Add(1)
	return nil
}

func newTestSessionService(t *testing.T) *sessionService {
	t.Helper()
	service, err := newSessionService(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func loadAppPlan(t *testing.T, service *sessionService, threadID domain.ThreadID) (session.LoadResult, session.ReplayPlan) {
	t.Helper()
	loader, err := session.NewLoader(service.repository)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loader.Load(context.Background(), threadID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := session.NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	return loaded, plan
}

package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestPostCompactRecoverySyncFailureKeepsCommittedCheckpoint(t *testing.T) {
	ctx := context.Background()
	original := readAppTestFile(t, "../session/testdata/current/auto_compaction_completed_openai.jsonl")
	suffix := readAppTestFile(t, "../session/testdata/current/post_compact_openai_auto_started_suffix.jsonl")
	prefix := append(bytes.Clone(prefixLines(original, 10)), suffix...)
	root, path, thread := installPreviewFixture(t, prefix)
	if preview, cleanupFailed, err := previewSession(ctx, root, thread, defaultPreviewDependencies()); err != nil || cleanupFailed || len(preview.Messages) == 0 {
		t.Fatal("read-only preview could not load the installed checkpoint", err)
	}
	assertRecoveryFileBytes(t, path, prefix)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	const model = "claude-test"
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: model, BaseURL: server.URL, APIKey: secret.New("fixture-key")}}
	settings, err := cfg.ResolveProvider()
	if err != nil {
		t.Fatal(err)
	}
	binding, err := bindProvider(cfg.Provider.Family)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := binding.open(cfg.Provider, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	provider := binding.bindSession(instance)

	service, err := openSessionService(root)
	if err != nil {
		t.Fatal(err)
	}
	defer service.close()
	service.recovery = &recoveryAttempt{}
	normalStart := service.start
	syncFailure := errors.New("fixture post-compact close sync failure")
	failing := &failingManagedJournal{appendErr: syncFailure}
	service.start = func(*session.JournalLease, session.Identity, uint64) (journalStartResult, error) {
		return journalStartResult{writer: failing}, nil
	}
	failed, err := service.resume(ctx, provider, model, thread)
	if !errors.Is(err, &fault.Error{Code: fault.CodeSessionWrite}) || !errors.Is(err, syncFailure) || failed.writer != nil ||
		failing.appendCalls.Load() != 1 || failing.closeCalls.Load() != 1 || !service.recovery.fatal() {
		t.Fatalf("uncertain observer compensation left a usable session: %v", err)
	}
	assertSessionLeaseAvailable(t, service, thread)
	assertRecoveryFileBytes(t, path, prefix)

	service.start = normalStart
	service.recovery = nil
	for range 2 {
		resumed, err := service.resume(ctx, provider, model, thread)
		if err != nil {
			t.Fatal(err)
		}
		if len(resumed.compactions) != 1 || resumed.compactions[0].sequence != 11 {
			t.Fatal("observer recovery changed the original checkpoint cursor")
		}
		if err := resumed.writer.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
	recovered, plan := loadToolLoopPlan(t, root, thread)
	if len(recovered.Records) != 14 || plan.InterruptedPostCompact != nil || plan.InterruptedCompaction != nil ||
		len(plan.Compactions) != 1 || plan.Compactions[0].Completed == nil || recovered.Records[13].EventKind != session.EventTurnFailed {
		t.Fatal("recovery did not close only the observer and interrupted turn")
	}
	closed, err := session.DecodePostCompactHookClosedPayload(recovered.Records[12])
	if err != nil || closed.State != protocol.PostCompactUncertain || closed.Code != domain.HookOutcomeUncertain || closed.Finished != 0 {
		t.Fatalf("wrong closure for a started item with no known run result: %+v, %v", closed, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, prefix) || requests.Load() != 0 {
		t.Fatal("recovery rewrote the committed checkpoint or used the Provider transport", err)
	}
}

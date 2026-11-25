package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestPromptSubmitRecoveryClosesJournalBeforeLoadingHooks(t *testing.T) {
	fixture, err := os.ReadFile("../session/testdata/current/prompt_submit_initial_session_start.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.SplitN(fixture, []byte{'\n'}, 6)
	prefix := append(bytes.Join(lines[:5], []byte{'\n'}), '\n')
	root, journalPath, thread := installPreviewFixture(t, prefix)
	workspace := privateAppTempDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("offline prompt recovery contacted the provider")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	cfg := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("mock-hook-recovery-key"),
	}}

	var previous []byte
	for range 2 {
		ctx := context.Background()
		resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{
			loadHooks: func(context.Context, string, string) (domain.HookSnapshot, error) {
				content := mustReadPromptSubmitJournal(t, journalPath)
				rows := bytes.Split(bytes.TrimSuffix(content, []byte{'\n'}), []byte{'\n'})
				if len(rows) != 8 || !bytes.HasPrefix(content, prefix) {
					t.Fatal("prompt and turn recovery did not precede configuration loading")
				}
				for index, kind := range []session.EventKind{session.EventPromptSubmitHookClosed, session.EventTurnFailed} {
					record, err := session.DecodeRecord(rows[6+index])
					if err != nil || record.EventKind != kind {
						t.Fatalf("unexpected recovery fact %d: %v", 6+index, err)
					}
				}
				record, decodeErr := session.DecodeRecord(rows[5])
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				finished, decodeErr := session.DecodeHookExecutionFinished(record)
				if decodeErr != nil || finished.Result.Origin != "recovery" ||
					finished.Result.Code != domain.HookOutcomeUncertain || finished.Result.ExitKnown {
					t.Fatalf("recovered run did not preserve uncertainty: %v", decodeErr)
				}
				return domain.NewHookSnapshot(nil, nil)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := resources.close(ctx); err != nil {
			t.Fatal(err)
		}
		_, replay := loadToolLoopPlan(t, root, thread)
		if replay.InterruptedPromptSubmit != nil || replay.InterruptedTail != nil || len(replay.NativeCommits) != 0 {
			t.Fatal("recovery left work open or wrote native user input")
		}
		current := mustReadPromptSubmitJournal(t, journalPath)
		if previous != nil && !bytes.Equal(previous, current) {
			t.Fatal("second recovery modified the already closed prompt journal")
		}
		previous = current
	}
}

func mustReadPromptSubmitJournal(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

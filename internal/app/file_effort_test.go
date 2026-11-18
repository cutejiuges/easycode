package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
)

type freezeGateBoundary struct {
	workspaceFileBoundary
	reads            atomic.Int32
	entered, release chan struct{}
}

func (f *freezeGateBoundary) ReadWorkspaceFile(ctx context.Context, path string, limit int) (string, int, error) {
	text, count, err := f.workspaceFileBoundary.ReadWorkspaceFile(ctx, path, limit)
	if f.reads.Add(1) == 1 {
		close(f.entered)
		select {
		case <-f.release:
		case <-ctx.Done():
			return "", count, ctx.Err()
		}
	}
	return text, count, err
}

func TestFilePreparationAndRetractionKeepContentSeparateFromStrictNextEffort(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			workspace := privateAppTempDir(t)
			path := filepath.Join(workspace, "file")
			write := func(s string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(s), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("FROZEN_PREP\n")
			bodies := make(chan []byte, 4)
			release := make(chan struct{})
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				n := calls.Add(1)
				bodies <- body
				if n == 1 {
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write(activeReasoningFixture(family, int(n), false))
			}))
			defer server.Close()
			cfg := reasoningFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("fixture-key")}})
			r, err := openChatResources(ctx, cfg, filepath.Join(privateAppTempDir(t), "sessions"), "", workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer r.close(context.Background())
			gate := &freezeGateBoundary{workspaceFileBoundary: r.workspace, entered: make(chan struct{}), release: make(chan struct{})}
			r.fileSource.workspace = gate
			if err := r.session.StartInteractive(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := appendInteractiveText(ctx, r.session, "seed"); err != nil {
				t.Fatal(err)
			}
			assertAppEffort(t, family, <-bodies, "high")
			prepared := make(chan struct {
				r   protocol.CommandResult
				err error
			}, 1)
			go func() {
				result, err := appendInteractiveText(ctx, r.session, "next @file")
				prepared <- struct {
					r   protocol.CommandResult
					err error
				}{result, err}
			}()
			<-gate.entered
			journalPath, _ := r.service.repository.JournalPath(r.identity.ThreadID)
			before, _ := os.ReadFile(journalPath)
			appEffortSet(t, r, "during-file-preparation", appEffortChoice(t, "low"))
			close(gate.release)
			queued := <-prepared
			if queued.err != nil || queued.r.Disposition() != protocol.CommandDispositionQueued {
				t.Fatal("preparation did not queue", queued.err)
			}
			appEffortSet(t, r, "after-file-queue", appEffortChoice(t, "high"))
			if result, err := r.session.Retract(ctx, []domain.InputID{queued.r.EntryID()}); err != nil || result.Disposition() != protocol.CommandDispositionRetracted {
				t.Fatal("atomic retraction failed", err)
			}
			write("FROZEN_RESUBMIT\n")
			resubmitted, err := appendInteractiveText(ctx, r.session, "next @file")
			if err != nil || resubmitted.EntryID() == queued.r.EntryID() {
				t.Fatal("resubmission reused input identity", err)
			}
			write("CHANGED_AFTER_RESUBMIT\n")
			appEffortSet(t, r, "latest-before-next-start", appEffortChoice(t, "low"))
			after, _ := os.ReadFile(journalPath)
			if !bytes.Equal(before, after) {
				t.Fatal("preparation, queue, retraction or effort wrote Session facts")
			}
			close(release)
			terminals := 0
			commits := map[domain.InputID]int{}
			for item := range r.session.Outputs() {
				if e, ok := item.TurnEvent(); ok {
					if e.Kind == protocol.EventTurnFailed {
						t.Fatalf("file effort turn failed: %s", e.Payload)
					}
					if e.Kind == protocol.EventUserInputCommitted {
						p, err := protocol.DecodeUserInputCommitted(e)
						if err != nil {
							t.Fatal(err)
						}
						commits[p.InputID]++
					}
					if e.Kind == protocol.EventTurnCompleted {
						terminals++
						if terminals == 2 {
							break
						}
					}
				}
			}
			second := <-bodies
			assertAppEffort(t, family, second, "low")
			if !bytes.Contains(second, []byte("FROZEN_RESUBMIT")) || bytes.Contains(second, []byte("FROZEN_PREP")) || bytes.Contains(second, []byte("CHANGED_AFTER_RESUBMIT")) || gate.reads.Load() != 2 || calls.Load() != 2 || commits[queued.r.EntryID()] != 0 || commits[resubmitted.EntryID()] != 1 {
				t.Fatal("retraction/freeze or strict-next capture failed", gate.reads.Load(), calls.Load())
			}
		})
	}
}

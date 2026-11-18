package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
)

type countingFileBoundary struct {
	workspaceFileBoundary
	reads, scans atomic.Int32
	bytes        atomic.Int64
}

func (f *countingFileBoundary) ReadWorkspaceFile(ctx context.Context, path string, limit int) (string, int, error) {
	f.reads.Add(1)
	text, count, err := f.workspaceFileBoundary.ReadWorkspaceFile(ctx, path, limit)
	f.bytes.Add(int64(count))
	return text, count, err
}
func (f *countingFileBoundary) DiscoverFileNames(ctx context.Context, publish func([]string, domain.FileDiscoveryReport) (bool, error)) (domain.FileDiscoveryReport, error) {
	f.scans.Add(1)
	return f.workspaceFileBoundary.DiscoverFileNames(ctx, publish)
}

func TestFileMentionsDualProviderFrozenSteeringReadAuthorityAndOfflineRestore(t *testing.T) {
	t.Setenv("HOME", privateAppTempDir(t))
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			workspace := privateAppTempDir(t)
			root := filepath.Join(privateAppTempDir(t), "sessions")
			write := func(path, text string) {
				t.Helper()
				full := filepath.Join(workspace, path)
				if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("a", "first\r\nFROZEN_INITIAL\nlast")
			write("docs/a b.md", "FROZEN_DOC\n")
			write(".gitignore", "hidden.txt\n")
			write("hidden.txt", "HIDDEN_MANUAL\n")
			write(".easycode/skills/demo/SKILL.md", "---\ndescription: Review files\n---\nFROZEN_SKILL $ARGUMENTS @missing-in-skill")
			requests := make(chan []byte, 8)
			release := make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				n := calls.Add(1)
				requests <- body
				if n == 1 {
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				var tools []writeScenarioCall
				if n == 1 {
					tools = []writeScenarioCall{{"Write", `{"file_path":"a","content":"MUST_NOT_WRITE"}`}}
				}
				serveWriteScenarioSample(w, family, int(n), tools)
			}))
			defer server.Close()
			cfg := reasoningFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("fixture-key")}})
			r, err := openChatResources(ctx, cfg, root, "", workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer r.close(context.Background())
			boundary := &countingFileBoundary{workspaceFileBoundary: r.workspace}
			r.fileSource.workspace = boundary
			journalPath, _ := r.service.repository.JournalPath(r.identity.ThreadID)
			before, _ := os.ReadFile(journalPath)
			query := fileQuery(t, "", 1, 1)
			if err := r.fileSource.QueryFiles(query); err != nil {
				t.Fatal(err)
			}
			found := awaitFileSnapshot(t, r.fileSource, func(v domain.FileSearchSnapshot) bool {
				return v.Identity() == query.Identity() && v.State() == domain.FileSearchComplete
			})
			for _, path := range found.Paths() {
				if path == "hidden.txt" {
					t.Fatal("ignored file was suggested")
				}
			}
			r.fileSource.CancelFiles(query.Identity())
			if boundary.reads.Load() != 0 || calls.Load() != 0 {
				t.Fatal("completion read bodies or sampled")
			}
			if err := r.session.StartInteractive(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := appendInteractiveText(ctx, r.session, "bad @missing"); err == nil {
				t.Fatal("missing source accepted")
			}
			after, _ := os.ReadFile(journalPath)
			if !bytes.Equal(before, after) || r.writer.Poisoned() {
				t.Fatal("preparation failure changed journal")
			}
			const raw = `$demo inspect @a#L2-8 @"docs/a b.md" @a#L1`
			first, err := appendInteractiveText(ctx, r.session, raw)
			if err != nil {
				t.Fatal(err)
			}
			body := <-requests
			assertAppEffort(t, family, body, "high")
			if bytes.Count(body, []byte("# Attached workspace file")) != 3 || !bytes.Contains(body, []byte("FROZEN_INITIAL")) || !bytes.Contains(body, []byte("FROZEN_SKILL")) || boundary.reads.Load() != 3 {
				t.Fatal("file grouping, Skill or ordering lost", boundary.reads.Load())
			}
			write("a", "FROZEN_STEERING\n")
			queued, err := appendInteractiveText(ctx, r.session, "follow @a @hidden.txt")
			if err != nil || queued.Disposition() != protocol.CommandDispositionQueued {
				t.Fatal("steering queue failed", err)
			}
			write("a", "CHANGED_AFTER_QUEUE\n")
			appEffortSet(t, r, "file-pending-low", appEffortChoice(t, "low"))
			close(release)
			committed := map[domain.InputID]protocol.UserInputCommittedPayload{}
			for item := range r.session.Outputs() {
				if e, ok := item.TurnEvent(); ok {
					if e.Kind == protocol.EventTurnFailed {
						t.Fatalf("file turn failed: %s", e.Payload)
					}
					if e.Kind == protocol.EventUserInputCommitted {
						p, err := protocol.DecodeUserInputCommitted(e)
						if err != nil {
							t.Fatal(err)
						}
						committed[p.InputID] = p
					}
					if e.Kind == protocol.EventTurnCompleted {
						break
					}
				}
			}
			appEffortIdle(t, r)
			second := <-requests
			assertAppEffort(t, family, second, "high")
			if calls.Load() != 2 || !bytes.Contains(second, []byte("FROZEN_STEERING")) || !bytes.Contains(second, []byte("HIDDEN_MANUAL")) || bytes.Contains(second, []byte("CHANGED_AFTER_QUEUE")) || !bytes.Contains(second, []byte("write_requires_full_read")) || committed[first.EntryID()].Files.Total() != 3 || committed[queued.EntryID()].Files.Total() != 2 || boundary.reads.Load() != 5 {
				t.Fatal("freeze, steering or Read authority boundary changed", boundary.reads.Load())
			}
			actual, _ := os.ReadFile(filepath.Join(workspace, "a"))
			if string(actual) != "CHANGED_AFTER_QUEUE\n" {
				t.Fatal("attachment granted Write")
			}
			page, err := r.transcriptQueries.ListTranscript(ctx, protocol.TranscriptListQuery{Mode: protocol.TranscriptMain, ThreadID: r.identity.ThreadID, Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			var savedSummary domain.FileSummary
			for _, item := range page.Items {
				if item.InputID == first.EntryID() {
					savedSummary = item.Files
					if !item.More || item.Files.Total() != 3 || strings.Contains(item.Text, "FROZEN_INITIAL") {
						t.Fatal("mainline leaked body or omitted details")
					}
					detail, err := r.transcriptQueries.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{ThreadID: r.identity.ThreadID, Cursor: item.Cursor, Source: protocol.TranscriptText, Limit: 64 << 10})
					if err != nil || !strings.Contains(detail.Text, "FROZEN_INITIAL") || !strings.Contains(detail.Text, "Source lines: 3; included: 2-3") {
						t.Fatal("frozen detail missing", err)
					}
				}
			}
			if savedSummary.Total() != 3 {
				t.Fatal("user transcript item missing")
			}
			thread := r.identity.ThreadID
			if err := r.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			original, _ := os.ReadFile(journalPath)
			for _, path := range []string{"a", "docs", "hidden.txt", ".easycode/skills"} {
				if err := os.RemoveAll(filepath.Join(workspace, path)); err != nil {
					t.Fatal(err)
				}
			}
			preview, cleanup, err := previewSession(ctx, root, thread, defaultPreviewDependencies())
			if err != nil || cleanup || len(preview.Messages) == 0 {
				t.Fatal("offline preview failed", err)
			}
			restored, err := openChatResources(ctx, cfg, root, string(thread), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.close(context.Background())
			if calls.Load() != 2 {
				t.Fatal("offline restore sampled")
			}
			var users []domain.SemanticMessage
			for _, turn := range restored.history.Turns {
				for _, m := range turn.Messages {
					if m.Role == domain.SemanticUser {
						users = append(users, m)
					}
				}
			}
			if len(users) != 2 || users[0].Text != raw || users[0].Files.Len() != 3 || users[1].Files.Items()[0].Content() != "FROZEN_STEERING\n" {
				t.Fatal("restored attachment facts changed")
			}
			restoredPage, err := restored.transcriptQueries.ListTranscript(ctx, protocol.TranscriptListQuery{Mode: protocol.TranscriptMain, ThreadID: thread, Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range restoredPage.Items {
				if item.InputID == first.EntryID() && !item.Files.Equal(savedSummary) {
					t.Fatal("live/restored summaries differ")
				}
			}
			now, _ := os.ReadFile(journalPath)
			if !bytes.Equal(original, now) {
				t.Fatal("restore rewrote journal")
			}
			if err := restored.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			_, plan := loadToolLoopPlan(t, root, thread)
			if len(plan.Tools) != 1 {
				t.Fatal("attachment created tool ledger", len(plan.Tools))
			}
		})
	}
}

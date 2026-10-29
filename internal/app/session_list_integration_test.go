//go:build darwin || linux

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
	"syscall"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/session/catalog"
)

func TestListThenResumeBothProviders(t *testing.T) {
	for _, fixture := range []struct {
		family      domain.ProviderFamily
		wire, model string
		serve       func(io.Writer, int)
	}{
		{domain.ProviderOpenAI, "responses", "gpt-test", writeOpenAIAppTurn},
		{domain.ProviderAnthropic, "messages", "claude-test", writeAnthropicAppTurn},
	} {
		t.Run(string(fixture.family), func(t *testing.T) {
			dir := privateAppTempDir(t)
			root := filepath.Join(privateAppTempDir(t), "sessions")
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fixture.serve(w, int(requests.Add(1)))
			}))
			defer server.Close()
			cfg := config.Config{Provider: config.Provider{Family: fixture.family, Model: fixture.model, APIKey: secret.New("list-test-secret"), BaseURL: server.URL}}
			created, err := openChatResources(context.Background(), cfg, root, "", dir)
			if err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, created, "private-list-prompt")
			id := created.identity.ThreadID
			if err := created.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			repository, err := session.OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = repository.Close() }()
			path, err := repository.JournalPath(id)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var out, diagnostics bytes.Buffer
			outcome := ListSessions(context.Background(), SessionListOptions{All: true, Limit: 20, SessionDataRoot: root, SessionCatalogPath: filepath.Join(dir, "state.sqlite"), Output: &out, ErrorOutput: &diagnostics})
			if outcome.ExitCode() != 0 || requests.Load() != 1 || !strings.Contains(out.String(), string(id)) || diagnostics.Len() != 0 {
				t.Fatalf("%+v requests=%d out=%q err=%q", outcome, requests.Load(), out.String(), diagnostics.String())
			}
			for _, forbidden := range []string{"private-list-prompt", "answer-1", "list-test-secret", server.URL, "encrypted", "signature"} {
				if strings.Contains(out.String(), forbidden) {
					t.Fatalf("list leaked %q", forbidden)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("list changed journal")
			}
			// 从可见条目提取身份，证明列表能直接定位既有恢复入口。
			var listed string
			for _, line := range strings.Split(out.String(), "\n") {
				if candidate, e := domain.ParseThreadID(line); e == nil {
					listed = string(candidate)
					break
				}
			}
			if listed != string(id) {
				t.Fatalf("listed identity=%q", listed)
			}
			wrong := cfg
			wrong.Provider.Model = "different-model"
			if rejected, err := openChatResources(context.Background(), wrong, root, listed, dir); err == nil {
				_ = rejected.close(context.Background())
				t.Fatal("incompatible model resumed")
			}
			if requests.Load() != 1 {
				t.Fatal("incompatible resume requested provider")
			}
			resumed, err := openChatResources(context.Background(), cfg, root, listed, dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(resumed.history.Turns) != 1 || resumed.history.Turns[0].UserText != "private-list-prompt" {
				t.Fatal("history was lost")
			}
			submitAppTurn(t, resumed, "next")
			if err := resumed.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, err = os.ReadFile(path)
			if err != nil || !bytes.HasPrefix(after, before) || requests.Load() != 2 {
				t.Fatal("resume rewrote history or request count changed")
			}
			lease, err := repository.Open(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := session.NewLoader().Load(context.Background(), lease)
			closeErr := lease.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("load=%v close=%v", err, closeErr)
			}
			for i, record := range loaded.Records {
				if record.Sequence != uint64(i+1) {
					t.Fatal("non-monotonic sequence")
				}
			}
		})
	}
}

func TestSessionListReportsBusyAcrossProcesses(t *testing.T) {
	dir := privateAppTempDir(t)
	root := filepath.Join(dir, "sessions")
	service, err := openSessionService(root)
	if err != nil {
		t.Fatal(err)
	}
	defer service.close()
	created, err := service.create(context.Background(), &fakeProviderFactory{family: domain.ProviderOpenAI}, "responses", "gpt-test", dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	options := SessionListOptions{All: true, Limit: 20, SessionDataRoot: root, SessionCatalogPath: filepath.Join(dir, "state.sqlite"), Output: &out, ErrorOutput: &diagnostics}
	if got := ListSessions(context.Background(), options); got.ExitCode() != 0 {
		t.Fatalf("%+v", got)
	}
	helper := startAppSessionHelper(t, root, created.identity.ThreadID)
	path, err := service.repository.JournalPath(created.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	diagnostics.Reset()
	if got := ListSessions(context.Background(), options); got.ExitCode() != 0 || !strings.Contains(out.String(), string(created.identity.ThreadID)) || !strings.Contains(diagnostics.String(), "storage-wide scan found 1 busy") {
		t.Fatalf("%+v %q %q", got, out.String(), diagnostics.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("busy listing changed bytes")
	}
	helper.stop(t)
	out.Reset()
	diagnostics.Reset()
	if got := ListSessions(context.Background(), options); got.ExitCode() != 0 || diagnostics.Len() != 0 {
		t.Fatalf("release=%+v %q", got, diagnostics.String())
	}
}

func TestSessionListInterruptedTailAndInvalidJournal(t *testing.T) {
	for _, kind := range []string{"unfinished", "half-line", "incomplete-batch", "invalid", "deleted"} {
		t.Run(kind, func(t *testing.T) {
			dir := privateAppTempDir(t)
			root := filepath.Join(dir, "sessions")
			service, err := openSessionService(root)
			if err != nil {
				t.Fatal(err)
			}
			defer service.close()
			created, err := service.create(context.Background(), &fakeProviderFactory{family: domain.ProviderOpenAI}, "responses", "gpt-test", dir)
			if err != nil {
				t.Fatal(err)
			}
			id := created.identity.ThreadID
			path, err := service.repository.JournalPath(id)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "unfinished" {
				turn, err := domain.GenerateTurnID()
				if err != nil {
					t.Fatal(err)
				}
				draft, err := session.NewTurnStartedDraft(turn)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := created.writer.AppendBatch(context.Background(), []session.RecordDraft{draft}); err != nil {
					t.Fatal(err)
				}
			}
			if err := created.writer.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var out, diagnostics bytes.Buffer
			options := SessionListOptions{All: true, Limit: 20, SessionDataRoot: root, SessionCatalogPath: filepath.Join(dir, "state.sqlite"), Output: &out, ErrorOutput: &diagnostics}
			if got := ListSessions(context.Background(), options); got.ExitCode() != 0 {
				t.Fatalf("initial=%+v", got)
			}
			before := append([]byte(nil), original...)
			switch kind {
			case "half-line":
				before = append(before, []byte(`{"schema":`)...)
			case "incomplete-batch":
				// 创建元数据 batch 共两条；仅保留首条可验证整个未完成尾 batch 被修复。
				before = append([]byte(nil), original[:bytes.IndexByte(original, '\n')+1]...)
			case "invalid":
				before = append(before, []byte("invalid-middle-record\n")...)
			}
			if kind == "deleted" {
				err = os.Remove(path)
			} else {
				err = os.WriteFile(path, before, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			out.Reset()
			diagnostics.Reset()
			got := ListSessions(context.Background(), options)
			if got.ExitCode() != 0 {
				t.Fatalf("%+v", got)
			}
			after, readErr := os.ReadFile(path)
			switch kind {
			case "half-line":
				if readErr != nil || !bytes.Equal(after, original) || !strings.Contains(out.String(), string(id)) {
					t.Fatal("repair failed")
				}
			case "incomplete-batch":
				if readErr != nil || len(after) != 0 || !strings.Contains(diagnostics.String(), "invalid") || strings.Contains(out.String(), string(id)) {
					t.Fatal("incomplete metadata was indexed")
				}
			case "deleted":
				if !os.IsNotExist(readErr) || strings.Contains(out.String(), string(id)) {
					t.Fatal("stale deleted row")
				}
			default:
				if readErr != nil || !bytes.Equal(after, before) {
					t.Fatal("listing changed complete/invalid records")
				}
				if kind == "unfinished" && !strings.Contains(out.String(), string(id)) {
					t.Fatal("unfinished turn disappeared")
				}
				if kind == "invalid" && (!strings.Contains(diagnostics.String(), "1 invalid") || strings.Contains(out.String(), string(id))) {
					t.Fatal("invalid journal not reported")
				}
			}
		})
	}
}

func TestSessionListRejectsUnsafeStore(t *testing.T) {
	dir := privateAppTempDir(t)
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(outside, []byte("private-target"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "state.sqlite")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	got := ListSessions(context.Background(), SessionListOptions{All: true, Limit: 20, SessionDataRoot: filepath.Join(dir, "sessions"), SessionCatalogPath: link, Output: &out, ErrorOutput: &diagnostics})
	if got.ExitCode() != 1 || out.Len() != 0 || strings.Contains(got.Failure.Message, "private-target") {
		t.Fatalf("%+v %q", got, out.String())
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "private-target" {
		t.Fatal("unsafe target changed")
	}
}

// syncFailureCatalog 在应用的协调边界注入 durable 失败，真实存储 owner 仍需释放。
type syncFailureCatalog struct{ *catalog.Catalog }

func (instance *syncFailureCatalog) Reconcile(context.Context, *session.Repository) (catalog.ReconciliationReport, error) {
	return catalog.ReconciliationReport{}, &os.PathError{Op: "sync", Path: "private-journal-path", Err: syscall.EIO}
}

func TestSessionListSyncFailureDoesNotPrintSuccessOrRetainLease(t *testing.T) {
	dir := privateAppTempDir(t)
	root := filepath.Join(dir, "sessions")
	database := filepath.Join(dir, "state.sqlite")
	service, err := openSessionService(root)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.create(context.Background(), &fakeProviderFactory{family: domain.ProviderOpenAI}, "responses", "gpt-test", dir)
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
	if err := service.close(); err != nil {
		t.Fatal(err)
	}
	var out, diagnostics bytes.Buffer
	got := listSessions(context.Background(), SessionListOptions{All: true, Limit: 20, SessionDataRoot: root, SessionCatalogPath: database, Output: &out, ErrorOutput: &diagnostics}, sessionListDependencies{
		openRepository:  session.OpenOrCreateRepository,
		closeRepository: func(r *session.Repository) error { return r.Close() },
		newCatalog: func(c catalog.Config) (sessionListCatalog, error) {
			instance, err := catalog.New(c)
			if err != nil {
				return nil, err
			}
			return &syncFailureCatalog{instance}, nil
		},
	})
	if got.ExitCode() != 1 || out.Len() != 0 || strings.Contains(got.Failure.Message, "private-journal-path") {
		t.Fatalf("%+v %q", got, out.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failure changed journal")
	}
	// 再次成功打开并查询，证明失败没有留下 Catalog 锁或资源 owner。
	if got := ListSessions(context.Background(), SessionListOptions{All: true, Limit: 20, SessionDataRoot: root, SessionCatalogPath: database, Output: &out, ErrorOutput: &diagnostics}); got.ExitCode() != 0 {
		t.Fatalf("retry=%+v", got)
	}
}

package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tui"
)

func installPreviewFixture(t *testing.T, data []byte) (string, string, domain.ThreadID) {
	t.Helper()
	first, err := session.DecodeRecord(bytes.SplitN(data, []byte{'\n'}, 2)[0])
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(privateAppTempDir(t), "sessions")
	repo, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repo.Create(context.Background(), first.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := repo.JournalPath(first.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return root, path, first.ThreadID
}

func TestPreviewDescriptorBoundPathReplacement(t *testing.T) {
	original, err := os.ReadFile("../session/testdata/current/input_only_openai.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root, path, thread := installPreviewFixture(t, original)
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err = os.WriteFile(outside, []byte("private outside"), 0600); err != nil {
		t.Fatal(err)
	}
	deps := defaultPreviewDependencies()
	deps.load = func(ctx context.Context, lease *session.JournalLease) (session.LoadResult, error) {
		if err := os.Rename(path, path+".held"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		return session.NewLoader().Load(ctx, lease)
	}
	result, _, err := previewSession(context.Background(), root, thread, deps)
	if err == nil {
		for _, entry := range result.Messages {
			if strings.Contains(entry.Message.Text, "private outside") {
				t.Fatal("read replaced symlink")
			}
		}
	}
	untouched, _ := os.ReadFile(outside)
	if string(untouched) != "private outside" {
		t.Fatal("outside file modified")
	}
	held, _ := os.ReadFile(path + ".held")
	if !bytes.Equal(held, original) {
		t.Fatal("held journal modified")
	}
	result, _, err = previewSession(context.Background(), root, thread, defaultPreviewDependencies())
	if err == nil || len(result.Messages) != 0 {
		t.Fatal("symlink journal accepted")
	}
}

func TestPreviewTitleUsesJournalInsteadOfPickerSnapshot(t *testing.T) {
	ctx := context.Background()
	base, err := os.ReadFile("../session/testdata/current/root.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	updates, err := os.ReadFile("../session/testdata/current/session_title_updates.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	cut := bytes.IndexByte(updates, '\n') + 1
	root, path, thread := installPreviewFixture(t, append(append([]byte(nil), base...), updates[:cut]...))
	discovery := pickerDiscovery{
		paths: dataPaths{sessionRoot: root, catalogPath: filepath.Join(filepath.Dir(root), "state.sqlite")},
		cwd:   t.TempDir(),
	}
	request := tui.PickerRequest{ID: 1, Operation: tui.PickerList}
	scoped, err := discovery.list(ctx, request)
	if err != nil || len(scoped.Entries) != 0 {
		t.Fatalf("current directory included an unrelated named session: %+v, %v", scoped, err)
	}
	request.ID++
	request.All = true
	snapshot, err := discovery.list(ctx, request)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Title != "复核 @src/parser" {
		t.Fatalf("initial discovery = %+v, %v", snapshot, err)
	}
	before, _, err := previewSession(ctx, root, thread, defaultPreviewDependencies())
	if err != nil || len(before.Messages) == 0 || before.Title != snapshot.Entries[0].Title {
		t.Fatalf("initial preview = %+v, %v", before, err)
	}
	content := append(append([]byte(nil), base...), updates...)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	after, cleanupFailed, err := previewSession(ctx, root, thread, defaultPreviewDependencies())
	if err != nil || cleanupFailed || after.Title != "解析 👩‍💻" || len(after.Messages) != len(before.Messages) || after.Truncated != before.Truncated {
		t.Fatalf("updated preview = %+v, %v", after, err)
	}
	for i, message := range after.Messages {
		if message.Message.Text != before.Messages[i].Message.Text {
			t.Fatalf("title changed preview content: %+v", message)
		}
	}
	request.ID++
	stillStale, err := discovery.list(ctx, request)
	if err != nil || len(stillStale.Entries) != 1 || stillStale.Entries[0].Title != "复核 @src/parser" {
		t.Fatalf("preview changed discovery = %+v, %v", stillStale, err)
	}
	request.ID++
	request.Operation = tui.PickerRefresh
	refreshed, err := discovery.list(ctx, request)
	if err != nil || len(refreshed.Entries) != 1 || refreshed.Entries[0].ThreadID != thread || refreshed.Entries[0].Title != after.Title {
		t.Fatalf("refreshed discovery = %+v, %v", refreshed, err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(unchanged, content) {
		t.Fatalf("discovery or preview changed journal: %v", err)
	}
}

// previewValidationBarrier 只在测试中阻塞离线 reader 的逐条取消检查。
type previewValidationBarrier struct {
	context.Context
	armed   bool
	checks  int
	entered chan struct{}
	release chan struct{}
}

func (c *previewValidationBarrier) Err() error {
	if c.armed {
		c.checks++
		if c.checks == 2 {
			close(c.entered)
			<-c.release
		}
	}
	return c.Context.Err()
}
func TestPreviewCancellationDuringNativeValidationReleasesLease(t *testing.T) {
	for _, fixture := range []string{"input_only_openai.jsonl", "input_only_anthropic.jsonl"} {
		t.Run(fixture, func(t *testing.T) {
			data, err := os.ReadFile("../session/testdata/current/" + fixture)
			if err != nil {
				t.Fatal(err)
			}
			root, path, thread := installPreviewFixture(t, data)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			barrier := &previewValidationBarrier{Context: ctx, entered: make(chan struct{}), release: make(chan struct{})}
			deps := defaultPreviewDependencies()
			deps.load = func(_ context.Context, lease *session.JournalLease) (session.LoadResult, error) {
				loaded, err := session.NewLoader().Load(ctx, lease)
				barrier.armed = true
				return loaded, err
			}
			type outcome struct {
				preview tui.SessionPreview
				cleanup bool
				err     error
			}
			done := make(chan outcome, 1)
			go func() { p, c, err := previewSession(barrier, root, thread, deps); done <- outcome{p, c, err} }()
			<-barrier.entered
			// 在释放 barrier 前不执行 Fatal，保证唯一 worker 必定被等待。
			repo, openErr := session.OpenOrCreateRepository(root)
			var busyErr error
			if openErr == nil {
				lease, err := repo.Open(context.Background(), thread)
				busyErr = err
				if lease != nil {
					lease.Close()
				}
			}
			cancel()
			var result outcome
			early := false
			select {
			case result = <-done:
				early = true
				t.Error("cancel abandoned native validator")
			default:
			}
			close(barrier.release)
			if !early {
				result = <-done
			}
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer repo.Close()
			if !session.IsJournalBusy(busyErr) {
				t.Fatal("validation lost exclusive lease", busyErr)
			}
			if !errors.Is(result.err, context.Canceled) || result.cleanup || len(result.preview.Messages) != 0 {
				t.Fatal("cancel leaked successful preview", result.err)
			}
			lease, err := repo.Open(context.Background(), thread)
			if err != nil {
				t.Fatal("cancel leaked lease", err)
			}
			if err = lease.Close(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, data) {
				t.Fatal("cancel changed journal", err)
			}
		})
	}
}

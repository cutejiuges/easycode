package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
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

func TestSessionPreviewCurrentFixturesAreReadOnly(t *testing.T) {
	files, err := filepath.Glob("../session/testdata/current/*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Base(file) == "invalid_version_fields.jsonl" {
			continue
		}
		t.Run(filepath.Base(file), func(t *testing.T) {
			original, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			root, path, thread := installPreviewFixture(t, original)
			for _, half := range []bool{false, true} {
				data := append([]byte(nil), original...)
				if half {
					data = append(data, []byte(`{"torn":`)...)
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				preview, cleanup, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
				valid := validPreviewFixture(filepath.Base(file))
				if cleanup || (err == nil) != valid {
					t.Fatalf("preview validity=%v: %v cleanup=%v", valid, err, cleanup)
				}
				if !valid {
					after, readErr := os.ReadFile(path)
					if readErr != nil || !bytes.Equal(after, data) || len(preview.Messages) != 0 {
						t.Fatal("rejected preview changed facts or exposed text")
					}
					continue
				}
				if half && !preview.Incomplete {
					t.Fatal("half-line not reported")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatal("preview modified journal")
				}
				repo, err := session.OpenOrCreateRepository(root)
				if err != nil {
					t.Fatal(err)
				}
				lease, err := repo.Open(context.Background(), thread)
				if err != nil {
					t.Fatal("preview retained lease", err)
				}
				loaded, err := session.NewLoader().Load(context.Background(), lease)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := session.NewReplayPlanner().Plan(loaded)
				if err != nil {
					t.Fatal(err)
				}
				records, err := orderedRestoreRecords(plan)
				if err != nil {
					t.Fatal(err)
				}
				meta := plan.SessionMetadata
				reader, err := storedHistoryReader(meta.Provider, meta.ProviderWire)
				if err != nil {
					t.Fatal(err)
				}
				source := provider.HistorySource{ThreadID: thread, Family: meta.Provider, Wire: meta.ProviderWire, Model: meta.Model}
				view, err := reader.ProjectStoredHistory(context.Background(), source, records)
				if err != nil {
					t.Fatal(err)
				}
				factory, err := newProviderResource(config.Config{Provider: config.Provider{Family: meta.Provider, Model: meta.Model, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-only")}})
				if err != nil {
					t.Fatal(err)
				}
				restored, err := factory.RestoreConversation(thread, records)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(view, restored.ProjectHistory()) {
					t.Fatal("offline projection differs from native restore")
				}
				want := boundSessionPreview(view)
				if !reflect.DeepEqual(preview.Messages, want.Messages) || preview.Truncated != want.Truncated {
					t.Fatal("bounded preview differs")
				}
				if len(view.Turns) > 0 && len(view.Turns[0].Messages) > 0 {
					view.Turns[0].Messages[0].Text = "mutated"
				}
				again, err := reader.ProjectStoredHistory(context.Background(), source, records)
				if err != nil || !reflect.DeepEqual(again, restored.ProjectHistory()) {
					t.Fatal("projection shares mutable data")
				}
				factory.Close()
				lease.Close()
				repo.Close()
			}
		})
	}
}

func TestSessionPreviewFailureAndCancellationKeepBytes(t *testing.T) {
	original, err := os.ReadFile("../session/testdata/current/root.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cancel", "close", "corrupt", "busy"} {
		t.Run(mode, func(t *testing.T) {
			root, path, thread := installPreviewFixture(t, original)
			deps := defaultPreviewDependencies()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "cancel":
				deps.load = func(ctx context.Context, lease *session.JournalLease) (session.LoadResult, error) {
					cancel()
					return session.NewLoader().Load(ctx, lease)
				}
			case "close":
				deps.closeLease = func(lease *session.JournalLease) error {
					return errors.Join(lease.Close(), errors.New("injected close failure"))
				}
			case "corrupt":
				data := bytes.Replace(original, []byte(`"seq":1`), []byte(`"seq":9`), 1)
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "busy":
				repo, err := session.OpenOrCreateRepository(root)
				if err != nil {
					t.Fatal(err)
				}
				defer repo.Close()
				lease, err := repo.Open(ctx, thread)
				if err != nil {
					t.Fatal(err)
				}
				defer lease.Close()
			}
			before, _ := os.ReadFile(path)
			preview, cleanup, err := previewSession(ctx, root, thread, deps)
			if err == nil || len(preview.Messages) != 0 || cleanup != (mode == "close") {
				t.Fatalf("failure result: %#v cleanup=%v err=%v", preview, cleanup, err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("failure modified journal")
			}
		})
	}
}

func TestSessionPreviewBudget(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", 32768), strings.Repeat("x", 32769), strings.Repeat("中", 11000), strings.Repeat("🙂", 8193)} {
		view := domain.SemanticHistoryView{Turns: []domain.SemanticTurn{{Messages: []domain.SemanticMessage{{Role: domain.SemanticUser, Text: text}}}}}
		got := boundSessionPreview(view)
		if len(got.Messages) != 1 || len(got.Messages[0].Message.Text) > 32768 || !utf8.ValidString(got.Messages[0].Message.Text) || got.Truncated != (len(text) > 32768) {
			t.Fatal("invalid budget result")
		}
	}
	messages := make([]domain.SemanticMessage, 130)
	for i := range messages {
		messages[i] = domain.SemanticMessage{Role: domain.SemanticAssistant, Text: "a"}
	}
	got := boundSessionPreview(domain.SemanticHistoryView{Turns: []domain.SemanticTurn{{Messages: messages}}})
	if len(got.Messages) != 128 || !got.Truncated {
		t.Fatal("message limit not enforced")
	}
}

// validPreviewFixture 显式区分完整 Provider 契约样本与仅供日志层重放的样本。
func validPreviewFixture(name string) bool {
	switch name {
	case "root.jsonl", "input_only_openai.jsonl", "input_only_anthropic.jsonl", "compaction_completed_openai.jsonl", "compaction_completed_anthropic.jsonl", "auto_compaction_completed_openai.jsonl", "auto_compaction_completed_anthropic.jsonl", "artifact_result_openai.jsonl", "artifact_result_anthropic.jsonl", "heterogeneous_exec_openai.jsonl", "heterogeneous_exec_anthropic.jsonl", "steering_tail_openai.jsonl", "steering_tail_anthropic.jsonl":
		return true
	default:
		return false
	}
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

func TestPreviewCommittedBatchPrefixesStayReadOnly(t *testing.T) {
	for _, name := range []string{"heterogeneous_exec_openai.jsonl", "heterogeneous_exec_anthropic.jsonl", "artifact_result_openai.jsonl", "artifact_result_anthropic.jsonl", "compaction_completed_openai.jsonl", "auto_compaction_completed_anthropic.jsonl"} {
		t.Run(name, func(t *testing.T) {
			original, err := os.ReadFile(filepath.Join("../session/testdata/current", name))
			if err != nil {
				t.Fatal(err)
			}
			root, path, thread := installPreviewFixture(t, original)
			lines := bytes.Split(bytes.TrimSuffix(original, []byte{'\n'}), []byte{'\n'})
			for i, line := range lines {
				record, err := session.DecodeRecord(line)
				if err != nil {
					t.Fatal(err)
				}
				if record.BatchIndex+1 != record.BatchSize {
					continue
				}
				prefix := append(bytes.Join(lines[:i+1], []byte{'\n'}), '\n')
				if err = os.WriteFile(path, prefix, 0600); err != nil {
					t.Fatal(err)
				}
				_, cleanup, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
				if err != nil || cleanup {
					t.Fatalf("committed prefix %d: %v", i+1, err)
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(after, prefix) {
					t.Fatal("preview compensated committed prefix")
				}
			}
		})
	}
}
func TestPreviewRejectsUnknownContractWithoutRepair(t *testing.T) {
	original, err := os.ReadFile("../session/testdata/current/root.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root, path, thread := installPreviewFixture(t, original)
	invalid, err := os.ReadFile("../session/testdata/current/invalid_version_fields.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	invalid = append(invalid, []byte(`{"torn":`)...)
	if err = os.WriteFile(path, invalid, 0600); err != nil {
		t.Fatal(err)
	}
	result, _, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
	if err == nil || len(result.Messages) != 0 {
		t.Fatal("unknown contract accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, invalid) {
		t.Fatal("invalid contract repaired")
	}
}

func TestSessionPreviewDoesNotAddEmptyTruncatedMessage(t *testing.T) {
	view := domain.SemanticHistoryView{Turns: []domain.SemanticTurn{{Messages: []domain.SemanticMessage{
		{Role: domain.SemanticUser, Text: strings.Repeat("a", 32767)},
		{Role: domain.SemanticAssistant, Text: "🙂"},
	}}}}
	got := boundSessionPreview(view)
	if !got.Truncated || len(got.Messages) != 1 || got.Messages[0].Truncated {
		t.Fatal("empty truncated message added or intact prefix marked")
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

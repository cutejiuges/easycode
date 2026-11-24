//go:build darwin || linux

package session

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestBranchPublicationRejectsReplacementAndDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name          string
		want          BranchPublication
		replaceBefore bool
		replaceAfter  bool
		conflict      bool
	}{
		{name: "date directory replaced before rename", want: BranchNotPublished, replaceBefore: true},
		{name: "date directory replaced after rename", want: BranchPublicationUncertain, replaceAfter: true},
		{name: "existing canonical entry", want: BranchNotPublished, conflict: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var directory string
			moveDirectory := func(string) {
				if err := os.Rename(directory, directory+"-moved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			var hooks securePathHooks
			if tc.replaceBefore {
				hooks.beforeBranchPublish = moveDirectory
			} else if tc.replaceAfter {
				hooks.afterBranchRename = moveDirectory
			}
			repo := openBranchTestRepository(t, hooks)
			source := openBranchTestSource(t, repo, readCurrentFixture(t, "root.jsonl"))
			id, err := domain.GenerateThreadID()
			if err != nil {
				t.Fatal(err)
			}
			filename, err := repo.JournalPath(id)
			if err != nil {
				t.Fatal(err)
			}
			directory = filepath.Dir(filename)
			stage, _, err := repo.PrepareBranch(ctx, source, id, "Route check")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = stage.Close() })
			if tc.conflict {
				if err := os.WriteFile(filename, []byte("existing journal"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := stage.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			if outcome, err := stage.Publish(); err == nil || outcome != tc.want {
				t.Fatalf("publish = %v, %v; want %v", outcome, err, tc.want)
			}
			if lease, err := stage.TakeLease(); err == nil {
				_ = lease.Close()
				t.Fatal("failed or uncertain outcome transferred lease")
			}
			staging := stage.stagingName
			if err := stage.Close(); err != nil {
				t.Fatal(err)
			}
			originalDirectory := directory
			if tc.replaceBefore || tc.replaceAfter {
				originalDirectory += "-moved"
			}
			if _, err := os.Stat(filepath.Join(originalDirectory, staging)); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("stage was not cleaned: %v", err)
			}
			if tc.conflict {
				if actual := readBranchTestJournal(t, repo, id); string(actual) != "existing journal" {
					t.Fatalf("conflicting object was modified: %q", actual)
				}
			} else if _, err := os.Stat(filename); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("replacement directory received a target: %v", err)
			}
			if tc.replaceAfter {
				held, err := os.ReadFile(filepath.Join(originalDirectory, filepath.Base(filename)))
				if err != nil || !bytes.HasPrefix(held, readCurrentFixture(t, "root.jsonl")) {
					t.Fatalf("possibly published bytes were deleted or changed: %v", err)
				}
			}
		})
	}
}

func TestBranchDirectorySyncFailureCanBeConfirmedOnExplicitResume(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("fixture directory sync failure")
	fail := true
	repo := openBranchTestRepository(t, securePathHooks{syncBranchDirectory: func(directory *os.File) error {
		if fail {
			return failure
		}
		return directory.Sync()
	}})
	source := openBranchTestSource(t, repo, readCurrentFixture(t, "root.jsonl"))
	stage, _, err := repo.PrepareBranch(ctx, source, testBranchID, "Recoverable branch")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Close() })
	if err := stage.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if publication, err := stage.Publish(); publication != BranchPublicationUncertain || !errors.Is(err, failure) {
		t.Fatalf("publish = %v, %v", publication, err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	before := readBranchTestJournal(t, repo, testBranchID)
	lease, err := repo.Open(ctx, testBranchID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	loaded, err := NewLoader().Load(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmBranchDurability(ctx, lease); !errors.Is(err, failure) {
		t.Fatalf("confirmation incorrectly passed: %v", err)
	}
	if after := readBranchTestJournal(t, repo, testBranchID); !bytes.Equal(after, before) {
		t.Fatal("failed confirmation changed the journal")
	}
	fail = false
	if err := repo.ConfirmBranchDurability(ctx, lease); err != nil {
		t.Fatal(err)
	}
	writer := startBranchTestWriter(t, lease, loaded)
	appendBranchTitle(t, writer, "Confirmed later", 12, testBranchID)
}

func TestBranchFailedStageSyncDoesNotPublish(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("fixture file sync failure")
	repo := openBranchTestRepository(t, securePathHooks{syncBranchFile: func(*os.File) error { return failure }})
	source := openBranchTestSource(t, repo, readCurrentFixture(t, "root.jsonl"))
	stage, _, err := repo.PrepareBranch(ctx, source, testBranchID, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Close() })
	if err := stage.Sync(ctx); !errors.Is(err, failure) {
		t.Fatalf("stage sync error = %v", err)
	}
	if publication, err := stage.Publish(); publication != BranchNotPublished || err == nil {
		t.Fatalf("unsynced stage publish = %v, %v", publication, err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if lease, err := repo.Open(ctx, testBranchID); !errors.Is(err, fs.ErrNotExist) {
		_ = lease.Close()
		t.Fatalf("failed file sync left canonical branch: %v", err)
	}
}

func TestBranchPrepareReportsUnsafeSourceAndUnconfirmedStageCleanup(t *testing.T) {
	ctx := context.Background()
	content := readCurrentFixture(t, "root.jsonl")
	repo := openBranchTestRepository(t, securePathHooks{})
	lease := installBranchJournal(t, repo, testThreadID, content)
	loaded, err := NewLoader().Load(ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	file, err := lease.transfer()
	if err != nil {
		t.Fatal(err)
	}
	target, err := repo.JournalPath(testBranchID)
	if err != nil {
		t.Fatal(err)
	}
	var substituted string
	var stageErr error
	underlying := &branchShortReadFile{File: file, onRead: func() {
		dir := filepath.Dir(target)
		entries, err := os.ReadDir(dir)
		if err != nil {
			stageErr = err
			return
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".branch-stage-") {
				substituted = filepath.Join(dir, entry.Name())
				if err := os.Rename(substituted, substituted+"-moved"); err != nil {
					stageErr = err
					return
				}
				stageErr = os.WriteFile(substituted, []byte("foreign staging entry"), 0o600)
				return
			}
		}
		stageErr = fs.ErrNotExist
	}}
	writer := startOwnedJournalWriter(underlying, loaded.Identity, loaded.NextSequence, loaded.committedBytes, time.Now, journalRequestCapacity)
	defer writer.Close(ctx)
	stage, _, err := repo.PrepareBranch(ctx, writer, testBranchID, "")
	if stageErr != nil || stage != nil || !IsBranchSourceUnsafe(err) || !IsBranchCleanupFailure(err) || !writer.Poisoned() {
		t.Fatalf("prepare distinguishing source and cleanup error: stage=%v, error=%v, injection=%v", stage, err, stageErr)
	}
	if body, err := os.ReadFile(substituted); err != nil || string(body) != "foreign staging entry" {
		t.Fatalf("cleanup changed the substitute: %q, %v", body, err)
	}
	if after := readBranchTestJournal(t, repo, testThreadID); !bytes.Equal(after, content) {
		t.Fatal("branch stage changed source bytes")
	}
}

type branchShortReadFile struct {
	*os.File
	onRead func()
}

func (f *branchShortReadFile) ReadAt([]byte, int64) (int, error) {
	f.onRead()
	return 0, io.EOF
}

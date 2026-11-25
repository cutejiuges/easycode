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

	"easycode/internal/domain"
)

const (
	testBranchID     domain.ThreadID = "00000000-0020-7000-8000-000000000020"
	testGrandchildID domain.ThreadID = "00000000-0021-7000-8000-000000000021"
)

func TestBranchLineageKeepsCanonicalRecordsAndRepairsOnlyCurrentTail(t *testing.T) {
	ctx := context.Background()
	content := readCurrentFixture(t, "branch_lineage.jsonl")
	root := readCurrentFixture(t, "root.jsonl")
	lines := bytes.Split(bytes.TrimSuffix(content, []byte{'\n'}), []byte{'\n'})
	for _, index := range []int{10, 12} {
		line := lines[index]
		record := decodeFixtureRecord(t, line)
		payload, err := DecodeThreadForkedPayload(record)
		if err != nil || payload.SourceSequence != uint64(index) {
			t.Fatalf("fork payload = %#v, %v", payload, err)
		}
		draft, err := NewThreadForkedDraft(record.ParentThreadID, payload)
		requireNoError(t, err)
		rebuilt, err := BuildRecord(Identity{record.SessionID, record.ThreadID}, draft, record.Sequence, record.Timestamp, record.BatchID, 0, 1)
		requireNoError(t, err)
		_, encoded, err := EncodeRecord(rebuilt)
		if err != nil || !bytes.Equal(line, encoded) {
			t.Fatalf("fork record canonical bytes changed: %v", err)
		}
	}
	if !bytes.HasPrefix(content, root) {
		t.Fatal("fixture does not embed the exact source bytes")
	}
	truncated := content[:len(content)-7]
	committed := bytes.Join(lines[:13], []byte{'\n'})
	committed = append(committed, '\n')
	repo := openBranchTestRepository(t, securePathHooks{})
	lease := installBranchJournal(t, repo, testGrandchildID, truncated)
	loader := NewLoader()
	loaded, err := loader.Load(ctx, lease)
	if err != nil || !loaded.NeedsRepair() {
		t.Fatalf("load current tail = %#v, %v", loaded.Repair, err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil || plan.Branch == nil || plan.Branch.ParentThreadID != testBranchID || plan.Branch.SourceSequence != 12 ||
		plan.Identity.SessionID != testSessionID || plan.Identity.ThreadID != testGrandchildID ||
		plan.SessionMetadata.RootThreadID != testThreadID || plan.Title != "Second branch" || len(plan.NativeCommits) != 2 {
		t.Fatalf("lineage plan identity = %#v, branch = %#v, title = %q, error = %v", plan.Identity, plan.Branch, plan.Title, err)
	}
	if err := repo.ConfirmBranchDurability(ctx, lease); err != nil {
		t.Fatal(err)
	}
	report, err := loader.Repair(ctx, lease, loaded)
	if err != nil || report.TruncatedFrom != int64(len(committed)) {
		t.Fatalf("repair = %#v, %v", report, err)
	}
	writer := startBranchTestWriter(t, lease, loaded)
	appendBranchTitle(t, writer, "After repair", 14, testGrandchildID)
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	after := readBranchTestJournal(t, repo, testGrandchildID)
	if !bytes.HasPrefix(after, committed) || len(after) == len(committed) {
		t.Fatal("repair or continuation changed the inherited or completed branch bytes")
	}
}

func TestBranchLoaderRejectsUnprovedLineageWithoutRepair(t *testing.T) {
	ctx := context.Background()
	root := readCurrentFixture(t, "root.jsonl")
	markers := bytes.Split(bytes.TrimSuffix(readCurrentFixture(t, "branch_invalid_markers.jsonl"), []byte{'\n'}), []byte{'\n'})
	branch := readCurrentFixture(t, "branch_lineage.jsonl")
	modified := bytes.Replace(branch, []byte("}\n"), []byte("} \n"), 1)
	if _, err := DecodeRecord(bytes.SplitN(modified, []byte{'\n'}, 2)[0]); err != nil {
		t.Fatalf("the whitespace edit should retain a valid record checksum: %v", err)
	}
	withMarker := func(line []byte) []byte {
		return append(append(bytes.Clone(root), line...), []byte("\n{\"seq\":12")...)
	}
	cases := []struct {
		name   string
		data   []byte
		target domain.ThreadID
	}{
		{"wrong source sequence", withMarker(markers[0]), testBranchID},
		{"invalid singleton boundary", withMarker(markers[1]), testBranchID},
		{"unexpected payload field", withMarker(markers[2]), testBranchID},
		{"wrong current parent", withMarker(markers[3]), testBranchID},
		{"same checksum but changed ancestor bytes", modified, testGrandchildID},
		{"missing named branch boundary", append(bytes.Clone(root), []byte("{\"seq\":11")...), testBranchID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := openBranchTestRepository(t, securePathHooks{})
			content := tc.data
			lease := installBranchJournal(t, repo, tc.target, content)
			loaded, err := NewLoader().Load(ctx, lease)
			if err == nil || loaded.NeedsRepair() {
				t.Fatalf("invalid lineage unexpectedly loadable: repair=%t, error=%v", loaded.NeedsRepair(), err)
			}
			if after := readBranchTestJournal(t, repo, tc.target); !bytes.Equal(content, after) {
				t.Fatal("invalid journal was modified")
			}
		})
	}
}

func TestBranchReplayRetainsAncestorArtifactError(t *testing.T) {
	repo := openBranchTestRepository(t, securePathHooks{})
	content := readCurrentFixture(t, "branch_invalid_ancestor_artifact.jsonl")
	lease := installBranchJournal(t, repo, testGrandchildID, content)
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatalf("fixture must pass loader and prefix verification before semantic replay: %v", err)
	}
	_, replayErr := NewReplayPlanner().Plan(loaded)
	_, metadataErr := NewReplayPlanner().PlanMetadata(loaded)
	for _, err := range []error{replayErr, metadataErr} {
		if err == nil || !strings.Contains(err.Error(), "artifact page has no matching committed reference") {
			t.Fatalf("ancestor artifact corruption was lost: %v", err)
		}
	}
}

func TestBranchPublicationUsesOneLeaseAndDoesNotDependOnParentProcess(t *testing.T) {
	ctx := context.Background()
	repo := openBranchTestRepository(t, securePathHooks{})
	root := readCurrentFixture(t, "root.jsonl")
	source := openBranchTestSource(t, repo, root)
	stage, loaded, err := repo.PrepareBranch(ctx, source, testBranchID, "")
	requireNoError(t, err)
	t.Cleanup(func() { _ = stage.Close() })
	locations, err := repo.EnumerateJournals(ctx)
	if err != nil || len(locations) != 1 || locations[0].ThreadID != testThreadID {
		t.Fatalf("staged candidate entered journal enumeration: %#v, %v", locations, err)
	}
	if lease, err := repo.Open(ctx, testBranchID); !errors.Is(err, fs.ErrNotExist) {
		_ = lease.Close()
		t.Fatalf("unpublished target opened: %v", err)
	}
	if err := stage.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if outcome, err := stage.Publish(); err != nil || outcome != BranchPublished {
		t.Fatalf("publish = %v, %v", outcome, err)
	}
	if lease, err := repo.Open(ctx, testBranchID); !IsJournalBusy(err) {
		_ = lease.Close()
		t.Fatalf("lease lost after publish: %v", err)
	}
	branch := readBranchTestJournal(t, repo, testBranchID)
	if !bytes.HasPrefix(branch, root) || len(branch) <= len(root) {
		t.Fatal("published journal does not preserve exact committed prefix")
	}
	if err := source.Close(ctx); err != nil {
		t.Fatal(err)
	}
	parent := startJournalLeaseHelperForThread(t, repo.RootPath(), testThreadID)
	lease, err := stage.TakeLease()
	requireNoError(t, err)
	writer := startBranchTestWriter(t, lease, loaded)
	appendBranchTitle(t, writer, "Renamed independently", 12, testBranchID)
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	resumed, err := repo.Open(ctx, testBranchID)
	requireNoError(t, err)
	defer resumed.Close()
	branchLoaded := loadFixtureJournal(t, ctx, resumed)
	plan, err := NewReplayPlanner().Plan(branchLoaded)
	if err != nil || plan.Title != "Renamed independently" || plan.Identity.ThreadID != testBranchID {
		t.Fatalf("resume with ancestor busy: identity=%#v title=%q err=%v", plan.Identity, plan.Title, err)
	}
	if _, err := repo.Open(ctx, testThreadID); !IsJournalBusy(err) {
		t.Fatalf("parent process no longer owns lease: %v", err)
	}
	parent.kill(t)
	parentLease, err := repo.Open(ctx, testThreadID)
	requireNoError(t, err)
	defer parentLease.Close()
	if !bytes.Equal(root, readBranchTestJournal(t, repo, testThreadID)) ||
		!bytes.HasPrefix(readBranchTestJournal(t, repo, testBranchID), branch) {
		t.Fatal("parent or child committed bytes changed")
	}
}

func TestBranchCopySeparatesTargetFailureAndUnsafeSource(t *testing.T) {
	ctx := context.Background()
	repo := openBranchTestRepository(t, securePathHooks{})
	content := readCurrentFixture(t, "root.jsonl")
	source := openBranchTestSource(t, repo, content)
	if _, err := source.branchPrefix(ctx, shortBranchDestination{}, HistoricalCut{}); !errors.Is(err, io.ErrShortWrite) || source.Poisoned() {
		t.Fatalf("short target write must preserve source: poison=%t, error=%v", source.Poisoned(), err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	if _, err := source.branchPrefix(cancelled, cancelBranchDestination{cancel}, HistoricalCut{}); !errors.Is(err, context.Canceled) || source.Poisoned() {
		t.Fatalf("cancelled copy must preserve source: poison=%t, error=%v", source.Poisoned(), err)
	}
	filename, _ := repo.JournalPath(testThreadID)
	if err := os.Truncate(filename, int64(len(content)-1)); err != nil {
		t.Fatal(err)
	}
	if stage, _, err := repo.PrepareBranch(ctx, source, testBranchID, ""); !IsBranchSourceUnsafe(err) || !source.Poisoned() || stage != nil {
		t.Fatalf("invalid physical cut did not stop source: poison=%t, error=%v", source.Poisoned(), err)
	}
	if actual := readBranchTestJournal(t, repo, testThreadID); !bytes.Equal(actual, content[:len(content)-1]) {
		t.Fatal("branch attempt modified damaged source bytes")
	}
}

type shortBranchDestination struct{}

func (shortBranchDestination) Write([]byte) (int, error) { return 0, nil }

type cancelBranchDestination struct{ cancel context.CancelFunc }

func (v cancelBranchDestination) Write(bytes []byte) (int, error) { v.cancel(); return len(bytes), nil }

func openBranchTestRepository(t *testing.T, hooks securePathHooks) *Repository {
	t.Helper()
	repo, err := NewRepository(filepath.Join(t.TempDir(), "sessions"))
	requireNoError(t, err)
	repo.hooks = hooks
	if err := repo.OpenOrCreate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func openBranchTestSource(t *testing.T, repo *Repository, input []byte) *JournalWriter {
	t.Helper()
	lease := installBranchJournal(t, repo, testThreadID, input)
	loaded := loadFixtureJournal(t, context.Background(), lease)
	return startBranchTestWriter(t, lease, loaded)
}

func installBranchJournal(t *testing.T, repo *Repository, id domain.ThreadID, content []byte) *JournalLease {
	t.Helper()
	lease, err := repo.Create(context.Background(), id)
	requireNoError(t, err)
	t.Cleanup(func() { _ = lease.Close() })
	file, _, err := lease.beginIO()
	requireNoError(t, err)
	defer lease.endIO()
	if count, err := file.Write(content); err != nil || count != len(content) {
		t.Fatalf("write fixture = %d/%d, %v", count, len(content), err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	return lease
}

func startBranchTestWriter(t *testing.T, lease *JournalLease, loaded LoadResult) *JournalWriter {
	t.Helper()
	writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	requireNoError(t, err)
	t.Cleanup(func() { _ = writer.Close(context.Background()) })
	return writer
}

func readBranchTestJournal(t *testing.T, repo *Repository, thread domain.ThreadID) []byte {
	t.Helper()
	filename, err := repo.JournalPath(thread)
	requireNoError(t, err)
	data := readSessionFixtureFile(t, filename)
	return data
}

func appendBranchTitle(t *testing.T, writer *JournalWriter, title domain.SessionTitle, sequence uint64, thread domain.ThreadID) {
	t.Helper()
	draft, err := NewSessionTitleUpdatedDraft(title)
	requireNoError(t, err)
	records, err := writer.AppendBatch(context.Background(), []RecordDraft{draft})
	if err != nil || len(records) != 1 || records[0].Sequence != sequence || records[0].ThreadID != thread {
		t.Fatalf("append branch title at %s/%d: %v", thread, sequence, err)
	}
}

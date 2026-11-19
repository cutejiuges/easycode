package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestSessionTitleReplaysLatestCanonicalRecord(t *testing.T) {
	ctx := context.Background()
	root := readCurrentFixture(t, "root.jsonl")
	updates := readCurrentFixture(t, "session_title_updates.jsonl")
	for _, line := range bytes.Split(bytes.TrimSuffix(updates, []byte{'\n'}), []byte{'\n'}) {
		record, err := DecodeRecord(line)
		if err != nil {
			t.Fatal(err)
		}
		_, encoded, err := EncodeRecord(record)
		if err != nil || !bytes.Equal(line, encoded) {
			t.Fatalf("title record canonical round trip = %s, %v", encoded, err)
		}
	}

	named := append(append([]byte{}, root...), updates...)
	for _, test := range []struct {
		name    string
		content []byte
		title   domain.SessionTitle
		nextSeq uint64
		torn    bool
	}{
		{name: "unnamed", content: root, nextSeq: 11},
		{name: "named", content: named, title: "解析 👩‍💻", nextSeq: 13},
		{name: "half-line", content: append(append([]byte{}, named...), []byte(`{"seq":13,"payload":`)...), title: "解析 👩‍💻", nextSeq: 13, torn: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository, lease := installCurrentFixture(t, test.content)
			defer repository.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(ctx, lease)
			if err != nil || loaded.NextSequence != test.nextSeq || loaded.NeedsRepair() != test.torn {
				t.Fatalf("title journal load = next seq %d, repair %v, error %v", loaded.NextSequence, loaded.NeedsRepair(), err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil || plan.Title != test.title {
				t.Fatalf("replay title = %q, %v", plan.Title, err)
			}
			assertTitleJournalBytes(t, repository, test.content)
		})
	}
}

func TestSessionTitleLoaderRejectsInvalidCompleteRecords(t *testing.T) {
	ctx := context.Background()
	prefix := readCurrentFixture(t, "root.jsonl")
	tests := []struct{ name, message string }{
		{"missing", "session title update payload is invalid"},
		{"null", "session title update payload is invalid"},
		{"duplicate", "duplicate or invalid JSON field"},
		{"unknown", "session title update payload is invalid"},
		{"noncanonical", "session title update payload is invalid"},
		{"oversized-batch", "session title record requires a single-record batch"},
		{"explicit-empty-turn", "session title record identity fields must be absent"},
		{"explicit-empty-parent", "session title record identity fields must be absent"},
		{"associated-turn", "session title record cannot contain a turn or parent thread identity"},
	}
	lines := bytes.Split(bytes.TrimSuffix(readCurrentFixture(t, "session_title_invalid.jsonl"), []byte{'\n'}), []byte{'\n'})
	if len(lines) != len(tests) {
		t.Fatal("invalid title fixture and cases are out of sync")
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := bytes.Join([][]byte{prefix, lines[index], {'\n'}}, nil)
			repository, lease := installCurrentFixture(t, content)
			defer repository.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(ctx, lease)
			if err == nil || !strings.Contains(err.Error(), test.message) || loaded.NeedsRepair() {
				t.Fatalf("invalid complete title load = repair %v, error %v", loaded.NeedsRepair(), err)
			}
			assertTitleJournalBytes(t, repository, content)
		})
	}
}

func TestSessionTitleSyncFailureWaitsForFinalSyncAndReplaysWrittenTitle(t *testing.T) {
	ctx := context.Background()
	prefix := readCurrentFixture(t, "root.jsonl")
	repository, lease := installCurrentFixture(t, prefix)
	defer repository.Close()
	loaded, err := NewLoader().Load(ctx, lease)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	handle, err := lease.transfer()
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	syncFailure := errors.New("fixture title append sync failure")
	file := &titleSyncGate{
		File: handle, appendFailure: syncFailure,
		appendEntered: make(chan struct{}), appendRelease: make(chan struct{}),
		closeEntered: make(chan struct{}), closeRelease: make(chan struct{}),
	}
	writer, err := startJournalWriter(file, loaded.Identity, loaded.NextSequence, func() time.Time { return time.Unix(0, 0).UTC() })
	if err != nil {
		_ = handle.Close()
		t.Fatal(err)
	}
	releaseAppend := sync.OnceFunc(func() { close(file.appendRelease) })
	releaseClose := sync.OnceFunc(func() { close(file.closeRelease) })
	defer func() {
		releaseAppend()
		releaseClose()
		_ = writer.Close(ctx)
	}()
	draft, err := NewSessionTitleUpdatedDraft("复核 @src/parser")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.AppendBatch(ctx, []RecordDraft{draft, draft}); err == nil || writer.Poisoned() {
		t.Fatalf("invalid mixed batch = %v, poisoned %v", err, writer.Poisoned())
	}
	result := make(chan error, 1)
	go func() {
		_, appendErr := writer.AppendBatch(ctx, []RecordDraft{draft})
		result <- appendErr
	}()
	<-file.appendEntered
	nextLine, _, _ := bytes.Cut(readCurrentFixture(t, "session_title_updates.jsonl"), []byte{'\n'})
	expected := bytes.Join([][]byte{prefix, nextLine, {'\n'}}, nil)
	assertTitleJournalBytes(t, repository, expected)
	select {
	case appendErr := <-result:
		t.Fatalf("append returned before Sync released: %v", appendErr)
	default:
	}
	releaseAppend()
	if err := <-result; !errors.Is(err, syncFailure) || !writer.Poisoned() {
		t.Fatalf("append after Sync failure = %v, poisoned %v", err, writer.Poisoned())
	}
	if _, err := writer.AppendBatch(ctx, []RecordDraft{draft}); err == nil {
		t.Fatal("poisoned writer accepted another title")
	}

	closeContext, cancel := context.WithCancel(ctx)
	defer cancel()
	closeResult := make(chan error, 1)
	go func() { closeResult <- writer.Close(closeContext) }()
	<-file.closeEntered
	cancel()
	if err := <-closeResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("close waiting for final Sync = %v", err)
	}
	select {
	case <-writer.done:
		t.Fatal("writer closed while final Sync was blocked")
	default:
	}
	if contender, err := repository.Open(ctx, testThreadID); !IsJournalBusy(err) {
		if contender != nil {
			_ = contender.Close()
		}
		t.Fatalf("acquire journal while final Sync is in flight = %v", err)
	}
	releaseClose()
	if err := writer.Close(ctx); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("final writer result = %v", err)
	}
	reopened, err := repository.Open(ctx, testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, err = NewLoader().Load(ctx, reopened)
	if err != nil || loaded.NeedsRepair() {
		t.Fatalf("reload after final Sync = repair %v, error %v", loaded.NeedsRepair(), err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil || plan.Title != "复核 @src/parser" || loaded.NextSequence != 12 {
		t.Fatalf("recovered durable title = %q, next seq %d, error %v", plan.Title, loaded.NextSequence, err)
	}
	assertTitleJournalBytes(t, repository, expected)
}

func assertTitleJournalBytes(t *testing.T, repository *Repository, expected []byte) {
	t.Helper()
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("unexpected journal bytes: got %d, want %d, error %v", len(actual), len(expected), err)
	}
}

type titleSyncGate struct {
	*os.File
	appendFailure                error
	appendEntered, appendRelease chan struct{}
	closeEntered, closeRelease   chan struct{}
	appendAttempted              bool
}

func (file *titleSyncGate) Sync() error {
	if !file.appendAttempted {
		file.appendAttempted = true
		close(file.appendEntered)
		<-file.appendRelease
		return file.appendFailure
	}
	close(file.closeEntered)
	<-file.closeRelease
	return file.File.Sync()
}

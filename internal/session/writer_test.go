package session

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJournalWriterSerializesConcurrentAppends(t *testing.T) {
	t.Parallel()
	dataRoot := t.TempDir()
	path := dataRoot + "/journal.jsonl"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := startJournalWriter(
		file,
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		1,
		func() time.Time { return time.Unix(0, 0).UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	const count = 64
	sequences := make(chan uint64, count)
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			records, appendErr := writer.AppendBatch(context.Background(), []RecordDraft{
				mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
			})
			if appendErr != nil {
				t.Errorf("AppendBatch() error = %v", appendErr)
				return
			}
			sequences <- records[0].Sequence
		}()
	}
	wait.Wait()
	close(sequences)
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	gotSequences := make([]int, 0, count)
	for sequence := range sequences {
		gotSequences = append(gotSequences, int(sequence))
	}
	sort.Ints(gotSequences)
	for index, sequence := range gotSequences {
		if sequence != index+1 {
			t.Fatalf("sequence[%d] = %d", index, sequence)
		}
	}

	read, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	scanner := bufio.NewScanner(read)
	sequence := uint64(1)
	for scanner.Scan() {
		record, decodeErr := DecodeRecord(scanner.Bytes())
		if decodeErr != nil {
			t.Fatalf("DecodeRecord(seq=%d) error = %v", sequence, decodeErr)
		}
		if record.Sequence != sequence || record.BatchID != sequence {
			t.Fatalf("record = %#v", record)
		}
		sequence++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if sequence != count+1 {
		t.Fatalf("decoded %d records, want %d", sequence-1, count)
	}
}

func TestJournalWriterAssignsBatchBoundaryAndResumeSequence(t *testing.T) {
	t.Parallel()
	file := &memoryJournalFile{}
	writer, err := startJournalWriter(
		file,
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		7,
		func() time.Time { return time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	records, err := writer.AppendBatch(context.Background(), []RecordDraft{
		mustDraft(t, EventProviderNativeCommit, testTurnID, NativeCommitPayload{
			Provider: "openai", Wire: "responses", PayloadVersion: 1, Payload: []byte(`{"shape":"text_sample"}`),
		}),
		mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Sequence != 7 || records[1].Sequence != 8 ||
		records[0].BatchID != 7 || records[1].BatchID != 7 ||
		records[0].BatchIndex != 0 || records[1].BatchIndex != 1 || records[0].BatchSize != 2 {
		t.Fatalf("records = %#v", records)
	}
	if file.syncCount != 1 {
		t.Fatalf("sync count = %d", file.syncCount)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if file.syncCount != 2 || file.closeCount != 1 {
		t.Fatalf("sync/close counts = %d/%d", file.syncCount, file.closeCount)
	}
}

func TestJournalWriterPoisonsAfterShortWrite(t *testing.T) {
	t.Parallel()
	file := &memoryJournalFile{shortWrite: true}
	writer := mustTestWriter(t, file)
	draft := []RecordDraft{mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{})}
	if _, err := writer.AppendBatch(context.Background(), draft); err == nil || !strings.Contains(err.Error(), "short write") {
		t.Fatalf("first AppendBatch() error = %v", err)
	}
	if !writer.Poisoned() {
		t.Fatal("writer was not poisoned")
	}
	if _, err := writer.AppendBatch(context.Background(), draft); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("second AppendBatch() error = %v", err)
	}
	if file.writeCount != 1 {
		t.Fatalf("write count = %d", file.writeCount)
	}
	if err := writer.Close(context.Background()); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("Close() error = %v", err)
	}
	if err := writer.Close(context.Background()); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("second Close() error = %v", err)
	}
	if file.closeCount != 1 {
		t.Fatalf("close count = %d", file.closeCount)
	}
}

func TestJournalWriterPoisonsAfterSyncFailure(t *testing.T) {
	t.Parallel()
	file := &memoryJournalFile{syncErr: errors.New("fixture sync failure")}
	writer := mustTestWriter(t, file)
	_, err := writer.AppendBatch(context.Background(), []RecordDraft{
		mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
	})
	if err == nil || !strings.Contains(err.Error(), "sync session batch") {
		t.Fatalf("AppendBatch() error = %v", err)
	}
	if !writer.Poisoned() {
		t.Fatal("writer was not poisoned")
	}
	_ = writer.Close(context.Background())
}

func TestJournalWriterRejectsInvalidConstructionAndClosedAppend(t *testing.T) {
	t.Parallel()
	if _, err := startJournalWriter(nil, Identity{}, 0, nil); err == nil {
		t.Fatal("startJournalWriter(nil) unexpectedly succeeded")
	}
	file := &memoryJournalFile{}
	writer := mustTestWriter(t, file)
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.AppendBatch(context.Background(), []RecordDraft{
		mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
	}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("AppendBatch() error = %v", err)
	}
}

func TestStartJournalWriterTransfersLeaseOnlyAfterValidation(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	owner, err := OpenOrCreateRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	competitor, err := OpenOrCreateRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.Close()
	lease, err := owner.Create(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StartJournalWriter(lease, Identity{}, 1); err == nil {
		t.Fatal("StartJournalWriter() accepted an invalid identity")
	}
	if _, err := competitor.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("invalid start consumed lease, competing error = %v", err)
	}

	writer, err := StartJournalWriter(
		lease,
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); !errors.Is(err, errJournalLeaseTransferred) {
		t.Fatalf("transferred lease Close() error = %v", err)
	}
	if _, err := competitor.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("writer did not retain lease, competing error = %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("second writer Close() error = %v", err)
	}
	reacquired, err := competitor.Open(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJournalWriterRejectsQueueFullBeforeAllocatingSequence(t *testing.T) {
	file := newBlockingJournalFile()
	writer, err := startJournalWriterWithCapacity(
		file,
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		1,
		func() time.Time { return time.Unix(0, 0).UTC() },
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	draft := []RecordDraft{mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{})}
	firstResult := make(chan appendResponse, 1)
	go func() {
		records, appendErr := writer.AppendBatch(context.Background(), draft)
		firstResult <- appendResponse{records: records, err: appendErr}
	}()
	<-file.started

	queuedResponse := make(chan appendResponse, 1)
	writer.requests <- writerRequest{drafts: []RecordDraft{draft[0].clone()}, response: queuedResponse}
	if _, err := writer.AppendBatch(context.Background(), draft); err == nil || !strings.Contains(err.Error(), "queue is full") {
		t.Fatalf("queue-full AppendBatch() error = %v", err)
	}
	close(file.release)
	if result := <-firstResult; result.err != nil || result.records[0].Sequence != 1 {
		t.Fatalf("first append = %#v", result)
	}
	if result := <-queuedResponse; result.err != nil || result.records[0].Sequence != 2 {
		t.Fatalf("queued append = %#v", result)
	}
	records, err := writer.AppendBatch(context.Background(), draft)
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Sequence != 3 {
		t.Fatalf("sequence after queue rejection = %d", records[0].Sequence)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestJournalWriterCancellationLinearizesAtAdmission(t *testing.T) {
	t.Run("before admission", func(t *testing.T) {
		file := &memoryJournalFile{}
		writer := mustTestWriter(t, file)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := writer.AppendBatch(ctx, []RecordDraft{
			mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
		}); err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("cancelled AppendBatch() error = %v", err)
		}
		if file.writeCount != 0 {
			t.Fatalf("write count = %d", file.writeCount)
		}
		if err := writer.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("after admission", func(t *testing.T) {
		file := newBlockingJournalFile()
		writer := mustTestWriter(t, file)
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan appendResponse, 1)
		go func() {
			records, appendErr := writer.AppendBatch(ctx, []RecordDraft{
				mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
			})
			result <- appendResponse{records: records, err: appendErr}
		}()
		<-file.started
		cancel()
		close(file.release)
		response := <-result
		if response.err != nil || len(response.records) != 1 || response.records[0].Sequence != 1 {
			t.Fatalf("admitted append = %#v", response)
		}
		if err := writer.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestJournalWriterCancelledCloseStillDrainsAndCloses(t *testing.T) {
	file := newBlockingJournalFile()
	writer := mustTestWriter(t, file)
	appendResult := make(chan error, 1)
	go func() {
		_, err := writer.AppendBatch(context.Background(), []RecordDraft{
			mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
		})
		appendResult <- err
	}()
	<-file.started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := writer.Close(ctx); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancelled Close() error = %v", err)
	}
	if _, err := writer.AppendBatch(context.Background(), []RecordDraft{
		mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
	}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("AppendBatch() after closing error = %v", err)
	}
	close(file.release)
	if err := <-appendResult; err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	file.mu.Lock()
	closeCount := file.closeCount
	file.mu.Unlock()
	if closeCount != 1 {
		t.Fatalf("close count = %d", closeCount)
	}
}

func mustTestWriter(t *testing.T, file journalFile) *JournalWriter {
	t.Helper()
	writer, err := startJournalWriter(
		file,
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		1,
		func() time.Time { return time.Unix(0, 0).UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	return writer
}

type memoryJournalFile struct {
	mu         sync.Mutex
	buffer     bytes.Buffer
	shortWrite bool
	syncErr    error
	closeErr   error
	writeCount int
	syncCount  int
	closeCount int
}

type blockingJournalFile struct {
	mu         sync.Mutex
	buffer     bytes.Buffer
	started    chan struct{}
	release    chan struct{}
	blockOnce  sync.Once
	syncCount  int
	closeCount int
}

func newBlockingJournalFile() *blockingJournalFile {
	return &blockingJournalFile{started: make(chan struct{}), release: make(chan struct{})}
}

func (file *blockingJournalFile) Write(value []byte) (int, error) {
	file.blockOnce.Do(func() {
		close(file.started)
		<-file.release
	})
	file.mu.Lock()
	defer file.mu.Unlock()
	return file.buffer.Write(value)
}

func (file *blockingJournalFile) Sync() error {
	file.mu.Lock()
	defer file.mu.Unlock()
	file.syncCount++
	return nil
}

func (file *blockingJournalFile) Close() error {
	file.mu.Lock()
	defer file.mu.Unlock()
	file.closeCount++
	return nil
}

func (file *memoryJournalFile) Write(value []byte) (int, error) {
	file.mu.Lock()
	defer file.mu.Unlock()
	file.writeCount++
	if file.shortWrite && len(value) > 0 {
		_, _ = file.buffer.Write(value[:len(value)-1])
		return len(value) - 1, nil
	}
	return file.buffer.Write(value)
}

func (file *memoryJournalFile) Sync() error {
	file.mu.Lock()
	defer file.mu.Unlock()
	file.syncCount++
	return file.syncErr
}

func (file *memoryJournalFile) Close() error {
	file.mu.Lock()
	defer file.mu.Unlock()
	file.closeCount++
	return file.closeErr
}

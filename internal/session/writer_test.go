package session

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
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
	writer, err := newJournalWriter(
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
			records, appendErr := writer.AppendBatch(context.Background(), []RecordDraft{{
				EventKind: EventTurnStarted, TurnID: testTurnID, Payload: TurnStartedPayload{},
			}})
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
	writer, err := newJournalWriter(
		file,
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		7,
		func() time.Time { return time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	records, err := writer.AppendBatch(context.Background(), []RecordDraft{
		{EventKind: EventProviderNativeCommit, TurnID: testTurnID, Payload: NativeCommitPayload{
			Provider: "openai", Wire: "responses", PayloadVersion: 1, Payload: []byte(`{"shape":"text_sample"}`),
		}},
		{EventKind: EventTurnCompleted, TurnID: testTurnID, Payload: TurnCompletedPayload{}},
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
	draft := []RecordDraft{{EventKind: EventTurnStarted, TurnID: testTurnID, Payload: TurnStartedPayload{}}}
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
}

func TestJournalWriterPoisonsAfterSyncFailure(t *testing.T) {
	t.Parallel()
	file := &memoryJournalFile{syncErr: errors.New("fixture sync failure")}
	writer := mustTestWriter(t, file)
	_, err := writer.AppendBatch(context.Background(), []RecordDraft{{
		EventKind: EventTurnStarted, TurnID: testTurnID, Payload: TurnStartedPayload{},
	}})
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
	if _, err := newJournalWriter(nil, Identity{}, 0, nil); err == nil {
		t.Fatal("newJournalWriter(nil) unexpectedly succeeded")
	}
	file := &memoryJournalFile{}
	writer := mustTestWriter(t, file)
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.AppendBatch(context.Background(), []RecordDraft{{
		EventKind: EventTurnStarted, TurnID: testTurnID, Payload: TurnStartedPayload{},
	}}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("AppendBatch() error = %v", err)
	}
}

func mustTestWriter(t *testing.T, file journalFile) *JournalWriter {
	t.Helper()
	writer, err := newJournalWriter(
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

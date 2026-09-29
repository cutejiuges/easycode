package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestLoaderLoadsValidBatches(t *testing.T) {
	t.Parallel()
	content := append(
		encodeTestBatch(t, 1, []RecordDraft{mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{})}),
		encodeTestBatch(t, 2, []RecordDraft{
			mustDraft(t, EventProviderNativeCommit, testTurnID, NativeCommitPayload{
				Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 1,
				Payload: json.RawMessage(`{"shape":"text_sample"}`),
			}),
			mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}),
		})...,
	)
	loader, repository := writeRawJournal(t, content)
	defer repository.Close()
	result, err := loader.Load(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 3 || result.NextSequence != 4 || result.Repair.Repaired {
		t.Fatalf("Load() = %#v", result)
	}
	for index, record := range result.Records {
		if record.Sequence != uint64(index+1) {
			t.Fatalf("record[%d] = %#v", index, record)
		}
	}
}

func TestLoaderRepairsTruncatedFinalLineIdempotently(t *testing.T) {
	t.Parallel()
	committed := encodeTestBatch(t, 1, []RecordDraft{mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{})})
	content := append(append([]byte(nil), committed...), []byte(`{"schema_version":1,"payload_version"`)...)
	loader, repository := writeRawJournal(t, content)
	defer repository.Close()
	first, err := loader.Load(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Repair.Repaired || first.Repair.Kind != RepairHalfLine ||
		first.Repair.RemovedBytes == 0 || len(first.Records) != 1 {
		t.Fatalf("first Load() = %#v", first)
	}
	second, err := loader.Load(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Repair.Repaired || len(second.Records) != 1 || second.NextSequence != 2 {
		t.Fatalf("second Load() = %#v", second)
	}
}

func TestLoaderRepairsWholeTrailingIncompleteBatch(t *testing.T) {
	t.Parallel()
	committed := encodeTestBatch(t, 1, []RecordDraft{mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{})})
	firstOfTwo := encodeTestRecord(t, 2, 2, 0, 2, mustDraft(
		t, EventProviderNativeCommit, testTurnID,
		NativeCommitPayload{Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 1, Payload: json.RawMessage(`{}`)},
	))
	content := append(append([]byte(nil), committed...), firstOfTwo...)
	content = append(content, '\n')
	loader, repository := writeRawJournal(t, content)
	defer repository.Close()
	result, err := loader.Load(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Repair.Repaired || result.Repair.Kind != RepairIncompleteBatch || len(result.Records) != 1 {
		t.Fatalf("Load() = %#v", result)
	}
	path, _ := repository.JournalPath(testThreadID)
	remaining, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(remaining, committed) {
		t.Fatal("repair changed bytes before the incomplete batch")
	}
}

func TestLoaderKeepsCommittedTurnStarted(t *testing.T) {
	t.Parallel()
	content := encodeTestBatch(t, 1, []RecordDraft{mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{})})
	loader, repository := writeRawJournal(t, content)
	defer repository.Close()
	result, err := loader.Load(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Repair.Repaired || len(result.Records) != 1 || result.Records[0].EventKind != EventTurnStarted {
		t.Fatalf("Load() = %#v", result)
	}
}

func TestLoaderRejectsAmbiguousEnvelopeAndMiddleCorruption(t *testing.T) {
	t.Parallel()
	valid := bytes.TrimSuffix(encodeTestBatch(t, 1, []RecordDraft{
		mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
	}), []byte{'\n'})
	fixtures := map[string][]byte{
		"duplicate":     bytes.Replace(valid, []byte(`"seq":1`), []byte(`"seq":1,"seq":1`), 1),
		"unknown":       bytes.Replace(valid, []byte(`,"checksum"`), []byte(`,"future":true,"checksum"`), 1),
		"trailing":      append(append([]byte(nil), valid...), []byte(` {}`)...),
		"invalid UTF-8": {0xff},
	}
	for name, line := range fixtures {
		name, line := name, line
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			content := append(append([]byte(nil), line...), '\n')
			loader, repository := writeRawJournal(t, content)
			defer repository.Close()
			if _, err := loader.Load(context.Background(), testThreadID); err == nil || !strings.Contains(err.Error(), "corrupted") {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}

	middle := append(encodeTestBatch(t, 1, []RecordDraft{
		mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
	}), []byte("{bad json}\n")...)
	middle = append(middle, encodeTestBatch(t, 2, []RecordDraft{
		mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "failed"}),
	})...)
	loader, repository := writeRawJournal(t, middle)
	defer repository.Close()
	if _, err := loader.Load(context.Background(), testThreadID); err == nil || !strings.Contains(err.Error(), "corrupted") {
		t.Fatalf("Load(middle corruption) error = %v", err)
	}
}

func TestLoaderRejectsChecksumSequenceAndIdentityDrift(t *testing.T) {
	t.Parallel()
	base := mustDecodedRecord(t, EventTurnStarted, TurnStartedPayload{})
	base.TurnID = testTurnID
	base.Sequence = 1
	base.BatchID = 1
	base.BatchIndex = 0
	base.BatchSize = 1
	base.Timestamp = time.Unix(0, 0).UTC()
	_, encoded, err := EncodeRecord(base)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("checksum", func(t *testing.T) {
		line := bytes.Replace(encoded, []byte(`"payload":{}`), []byte(`"payload":{"changed":true}`), 1)
		loader, repository := writeRawJournal(t, append(line, '\n'))
		defer repository.Close()
		if _, err := loader.Load(context.Background(), testThreadID); err == nil || !strings.Contains(err.Error(), "checksum") {
			t.Fatalf("Load() error = %v", err)
		}
	})

	for name, mutate := range map[string]func(*Record){
		"sequence": func(record *Record) { record.Sequence, record.BatchID = 2, 2 },
		"thread": func(record *Record) {
			record.ThreadID = domain.ThreadID("00000000-0003-7000-8000-000000000004")
		},
		"session": func(record *Record) {
			record.SessionID = domain.SessionID("00000000-0004-7000-8000-000000000005")
		},
	} {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			record := base
			mutate(&record)
			_, line, encodeErr := EncodeRecord(record)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			content := append([]byte(nil), encoded...)
			content = append(content, '\n')
			if name == "session" {
				lineRecord := record
				lineRecord.Sequence, lineRecord.BatchID = 2, 2
				_, line, encodeErr = EncodeRecord(lineRecord)
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				content = append(content, line...)
				content = append(content, '\n')
			} else {
				content = append([]byte(nil), line...)
				content = append(content, '\n')
			}
			loader, repository := writeRawJournal(t, content)
			defer repository.Close()
			if _, err := loader.Load(context.Background(), testThreadID); err == nil || !strings.Contains(err.Error(), "corrupted") {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func encodeTestBatch(t *testing.T, start uint64, drafts []RecordDraft) []byte {
	t.Helper()
	var content bytes.Buffer
	for index, draft := range drafts {
		content.Write(encodeTestRecord(t, start+uint64(index), start, uint32(index), uint32(len(drafts)), draft))
		content.WriteByte('\n')
	}
	return content.Bytes()
}

func encodeTestRecord(
	t *testing.T,
	sequence uint64,
	batchID uint64,
	batchIndex uint32,
	batchSize uint32,
	draft RecordDraft,
) []byte {
	t.Helper()
	record, err := BuildRecord(
		Identity{SessionID: testSessionID, ThreadID: testThreadID},
		draft, sequence, time.Unix(0, 0).UTC(), batchID, batchIndex, batchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, encoded, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

type testJournalLoader struct {
	loader *Loader
	lease  *JournalLease
}

func (loader *testJournalLoader) Load(ctx context.Context, _ domain.ThreadID) (LoadResult, error) {
	return loader.loader.Load(ctx, loader.lease)
}

func writeRawJournal(t *testing.T, content []byte) (*testJournalLoader, *Repository) {
	t.Helper()
	repository, err := OpenOrCreateRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repository.Create(context.Background(), testThreadID)
	if err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	lease, err = repository.Open(context.Background(), testThreadID)
	if err != nil {
		_ = repository.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close() })
	return &testJournalLoader{loader: NewLoader(), lease: lease}, repository
}

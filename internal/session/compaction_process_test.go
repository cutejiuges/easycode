package session

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/protocol"
)

func TestCompactionLeaseCrashAndMonotonicRecovery(t *testing.T) {
	fixture := readCurrentFixture(t, "compaction_completed_openai.jsonl")
	lines := bytes.Split(bytes.TrimSuffix(fixture, []byte{'\n'}), []byte{'\n'})
	first, err := DecodeRecord(lines[0])
	if err != nil {
		t.Fatal(err)
	}
	thread := first.ThreadID
	prefix := append(bytes.Join(lines[:len(lines)-1], []byte{'\n'}), '\n')
	torn := append(append([]byte(nil), prefix...), lines[len(lines)-1][:32]...)
	root := filepath.Join(t.TempDir(), "sessions")
	repository, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	lease, err := repository.Create(context.Background(), thread)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	path, _ := repository.JournalPath(thread)
	if err := os.WriteFile(path, torn, 0600); err != nil {
		t.Fatal(err)
	}
	helper := startJournalLeaseHelperForThread(t, root, thread)
	competitor, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.Close()
	if other, err := competitor.Open(context.Background(), thread); err == nil {
		other.Close()
		t.Fatal("compaction lease bypassed")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, torn) {
		t.Fatal("contender repaired owned journal")
	}
	helper.kill(t)
	lease, err = repository.Open(context.Background(), thread)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	loader := NewLoader()
	loaded, err := loader.Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if plan.InterruptedCompaction == nil || loaded.NextSequence != uint64(len(lines)) {
		t.Fatal("interrupted compaction lost")
	}
	if _, err := loader.Repair(context.Background(), lease, loaded); err != nil {
		t.Fatal(err)
	}
	failed, err := NewCompactionFailedDraft(CompactionFailedPayload{ID: plan.InterruptedCompaction.Started.ID, Code: protocol.CompactionFailureInterrupted, UsageState: "unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	// 复用 writer 的既有恢复入口，接续 sequence 必须保持单调。
	writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		t.Fatal(err)
	}
	records, err := writer.AppendBatch(context.Background(), []RecordDraft{failed})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Sequence != uint64(len(lines)) {
		t.Fatal("sequence not monotonic")
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.HasPrefix(after, prefix) {
		t.Fatal("recovery rewrote committed facts")
	}
}

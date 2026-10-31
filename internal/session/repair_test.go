package session

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

func tornRepairFixture(t *testing.T) (*Loader, *JournalLease, LoadResult, string, []byte) {
	t.Helper()
	prefix := encodeTestBatch(t, 1, []RecordDraft{mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{})})
	original := append(append([]byte(nil), prefix...), []byte(`{"incomplete":`)...)
	harness, repository := writeRawJournal(t, original)
	t.Cleanup(func() { _ = harness.lease.Close(); _ = repository.Close() })
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := harness.loader.Load(context.Background(), harness.lease)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.NeedsRepair() || loaded.Repair.Repaired {
		t.Fatal("read-only scan did not retain pending tail")
	}
	return harness.loader, harness.lease, loaded, path, original
}

func TestLoadSemanticFailureWithTornTailDoesNotMutate(t *testing.T) {
	_, lease, loaded, path, original := tornRepairFixture(t)
	if _, err := NewReplayPlanner().Plan(loaded); err == nil {
		t.Fatal("missing metadata accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("semantic rejection changed bytes: %v", err)
	}
	if _, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence); err == nil {
		t.Fatal("writer bypassed pending repair")
	}
}

func TestRepairAuthorityAndCancellation(t *testing.T) {
	loader, lease, loaded, path, original := tornRepairFixture(t)
	_, other, _, _, _ := tornRepairFixture(t)
	if _, err := loader.Repair(context.Background(), other, loaded); err == nil {
		t.Fatal("cross-lease repair accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader.Repair(ctx, lease, loaded); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("cancel changed bytes")
	}
	// 公共报告与 records 不能改变私有修复位置。
	loaded.Repair.TruncatedFrom = 0
	loaded.Records = nil
	report, err := loader.Repair(context.Background(), lease, loaded)
	if err != nil || !report.Repaired || report.RemovedBytes != 14 {
		t.Fatalf("repair = %+v, %v", report, err)
	}
	if _, err := loader.Repair(context.Background(), lease, loaded); err == nil {
		t.Fatal("duplicate repair accepted")
	}
	next, err := loader.Load(context.Background(), lease)
	if err != nil || next.NeedsRepair() || next.NextSequence != 2 {
		t.Fatalf("next scan = %+v, %v", next, err)
	}
}

func TestRepairRejectsChangedSource(t *testing.T) {
	loader, lease, loaded, path, original := tornRepairFixture(t)
	changed := append([]byte(nil), original...)
	changed[len(changed)-1] = 'x'
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Repair(context.Background(), lease, loaded); err == nil {
		t.Fatal("same-size mutation accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, changed) {
		t.Fatal("stale repair changed bytes")
	}
}

type injectedRepair struct {
	truncateErr, syncErr error
	onTruncate           func()
	entered, release     chan struct{}
}

func (op injectedRepair) Truncate(file *os.File, offset int64) error {
	if op.onTruncate != nil {
		op.onTruncate()
	}
	if op.truncateErr != nil {
		return op.truncateErr
	}
	return file.Truncate(offset)
}
func (op injectedRepair) Sync(file *os.File) error {
	if op.entered != nil {
		close(op.entered)
		<-op.release
	}
	if op.syncErr != nil {
		return op.syncErr
	}
	return file.Sync()
}

func TestRepairFailurePoisonsLease(t *testing.T) {
	for _, phase := range []string{"truncate", "sync"} {
		t.Run(phase, func(t *testing.T) {
			loader, lease, loaded, _, _ := tornRepairFixture(t)
			op := injectedRepair{}
			if phase == "truncate" {
				op.truncateErr = errors.New("injected")
			} else {
				op.syncErr = errors.New("injected")
			}
			if _, err := loader.repairWith(context.Background(), lease, loaded, op); err == nil {
				t.Fatal("fault ignored")
			}
			if _, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence); err == nil {
				t.Fatal("poisoned lease transferred")
			}
			if _, err := loader.Load(context.Background(), lease); err == nil {
				t.Fatal("poisoned lease reused")
			}
		})
	}
}

func TestAdmittedRepairWaitsForSyncDespiteCancellation(t *testing.T) {
	loader, lease, loaded, _, _ := tornRepairFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op := injectedRepair{onTruncate: cancel, entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := loader.repairWith(ctx, lease, loaded, op); done <- err; close(done) }()
	<-op.entered
	if _, err := lease.transfer(); err == nil {
		t.Fatal("transferred during Sync")
	}
	select {
	case <-done:
		t.Fatal("returned before Sync")
	default:
	}
	close(op.release)
	if err := <-done; err != nil {
		t.Fatalf("admitted repair returned cancellation: %v", err)
	}
}

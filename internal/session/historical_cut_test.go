//go:build darwin || linux

package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestHistoricalBranchKeepsExactCutAndFullSourceWitness(t *testing.T) {
	ctx := context.Background()
	repo := openBranchTestRepository(t, securePathHooks{})
	content := readCurrentFixture(t, "task_openai_checkpoints.jsonl")
	first, _, _ := bytes.Cut(content, []byte{'\n'})
	metadata := decodeFixtureRecord(t, first)
	lease := installBranchJournal(t, repo, metadata.ThreadID, content)
	loaded := loadFixtureJournal(t, ctx, lease)
	source := startBranchTestWriter(t, lease, loaded)
	cuts, more := readHistoricalCuts(t, source)
	if len(cuts) != 3 || more || cuts[0].Number() != 2 || cuts[0].sequence != 10 || cuts[0].InputSequence() != 13 {
		t.Fatalf("historical cuts = %v, more=%t", cuts, more)
	}
	if after := readBranchTestJournal(t, repo, metadata.ThreadID); !bytes.Equal(after, content) {
		t.Fatal("reading candidates changed the source journal")
	}

	appendBranchTitle(t, source, "Entire source", loaded.NextSequence, metadata.ThreadID)
	stage, _, err := repo.PrepareHistoricalBranch(ctx, source, testBranchID, "", cuts[0])
	if stage != nil || !IsHistoricalCutStale(err) || source.Poisoned() {
		t.Fatalf("outdated selection = stage %v, poison %t, error %v", stage, source.Poisoned(), err)
	}

	cuts, _ = readHistoricalCuts(t, source)
	stage, child, err := repo.PrepareHistoricalBranch(ctx, source, testBranchID, "", cuts[0])
	requireNoError(t, err)
	t.Cleanup(func() { _ = stage.Close() })
	childPlan, err := NewReplayPlanner().Plan(child)
	if err != nil || len(childPlan.Compactions) != 1 || childPlan.Title != "Conversation (Branch)" ||
		childPlan.Branch == nil || childPlan.Branch.SourceSequence != 10 || len(child.Records) != 11 {
		t.Fatalf("historical child = %d records, window=%d, title=%q, branch=%v, error=%v",
			len(child.Records), len(childPlan.Compactions), childPlan.Title, childPlan.Branch, err)
	}
	if err := stage.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if published, err := stage.Publish(); err != nil || published != BranchPublished {
		t.Fatalf("publish historical branch = %v, %v", published, err)
	}
	prefix := bytes.Join(bytes.SplitN(content, []byte{'\n'}, 11)[:10], []byte{'\n'})
	prefix = append(prefix, '\n')
	if actual := readBranchTestJournal(t, repo, testBranchID); !bytes.HasPrefix(actual, prefix) {
		t.Fatal("published journal did not preserve the exact retained source bytes")
	}
	childLease, err := stage.TakeLease()
	requireNoError(t, err)
	childFile, err := childLease.transfer()
	requireNoError(t, err)
	// 列表上限不需要为每个合成回合执行真实磁盘同步。
	childWriter := startOwnedJournalWriter(historicalListFixtureFile{childFile}, child.Identity, child.NextSequence,
		child.committedBytes, time.Now, journalRequestCapacity)
	t.Cleanup(func() { _ = childWriter.Close(ctx) })
	turn, input := appendHistoricalFailedTurn(t, childWriter, 1, 2)
	appendHistoricalFailedTurn(t, childWriter, 2, 0)
	steered := appendHistoricalSteeredTurn(t, childWriter)
	childCuts, more := readHistoricalCuts(t, childWriter)
	if len(childCuts) != 2 || more || childCuts[0].TurnID() != turn || childCuts[0].InputID() != input ||
		childCuts[0].Number() != 1 || childCuts[0].sequence != 11 || !childCuts[0].HasMultipleInputs() ||
		childCuts[1].Number() != 3 || childCuts[1].TurnID() != steered || childCuts[1].InputID() != domain.InputID(steered) {
		t.Fatalf("current branch cuts = %v, more=%t", childCuts, more)
	}
	for number := 4; number <= 202; number++ {
		appendHistoricalFailedTurn(t, childWriter, number, 1)
	}
	childCuts, more = readHistoricalCuts(t, childWriter)
	if !more || len(childCuts) != 200 || childCuts[0].Number() != 3 || childCuts[199].Number() != 202 {
		t.Fatalf("bounded child cuts = %d, more=%t, expected turns 3 through 202", len(childCuts), more)
	}
	for index, cut := range childCuts {
		if cut.Number() != uint64(index)+3 {
			t.Fatalf("historical list out of order at index %d: turn %d", index, cut.Number())
		}
	}

	beforeDamage := readBranchTestJournal(t, repo, metadata.ThreadID)
	filename, err := repo.JournalPath(metadata.ThreadID)
	requireNoError(t, err)
	file, err := os.OpenFile(filename, os.O_WRONLY, 0)
	requireNoError(t, err)
	_, writeErr := file.WriteAt([]byte{'!'}, int64(len(beforeDamage)-1))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	damaged := readBranchTestJournal(t, repo, metadata.ThreadID)
	stage, _, err = repo.PrepareHistoricalBranch(ctx, source, testGrandchildID, "", cuts[0])
	if stage != nil || !IsBranchSourceUnsafe(err) || !source.Poisoned() {
		t.Fatalf("discarded suffix damage = stage %v, poison %t, error %v", stage, source.Poisoned(), err)
	}
	if actual := readBranchTestJournal(t, repo, metadata.ThreadID); !bytes.Equal(actual, damaged) {
		t.Fatal("failed creation repaired or rewrote the source")
	}
}

func readHistoricalCuts(t *testing.T, writer *JournalWriter) ([]HistoricalCut, bool) {
	t.Helper()
	loaded, err := writer.ReadHistorySnapshot(context.Background())
	requireNoError(t, err)
	plan, err := NewReplayPlanner().PlanContext(context.Background(), loaded)
	requireNoError(t, err)
	return plan.HistoricalCuts()
}

func appendHistoricalFailedTurn(t *testing.T, writer *JournalWriter, number, inputs int) (domain.TurnID, domain.InputID) {
	t.Helper()
	ctx := context.Background()
	turn := domain.TurnID(fmt.Sprintf("01900000-0000-7000-8000-%012x", number))
	start := mustDraft(t, EventTurnStarted, turn, TurnStartedPayload{})
	if _, err := writer.AppendBatch(ctx, []RecordDraft{start}); err != nil {
		t.Fatal(err)
	}
	ids := make([]domain.InputID, inputs)
	for index := range ids {
		ids[index] = domain.InputID(fmt.Sprintf("01900000-0001-7000-8000-%012x", number*2+index))
	}
	var first domain.InputID
	if len(ids) != 0 {
		first = ids[0]
		association, err := domain.NewInputAssociation(turn, domain.InputInitial, 0, ids)
		requireNoError(t, err)
		inputsDraft, err := NewTurnInputsCommittedDraft(association)
		requireNoError(t, err)
		native := mustDraft(t, EventProviderNativeCommit, turn, validNativeCommit())
		if _, err := writer.AppendBatch(ctx, []RecordDraft{inputsDraft, native}); err != nil {
			t.Fatal(err)
		}
	}
	appendHistoricalFailure(t, writer, turn)
	return turn, first
}

func appendHistoricalSteeredTurn(t *testing.T, writer *JournalWriter) domain.TurnID {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(readCurrentFixture(t, "stop_compound.jsonl")), []byte{'\n'})
	var turn domain.TurnID
	var batch []RecordDraft
	for _, line := range lines[2:] {
		record := decodeFixtureRecord(t, line)
		turn = record.TurnID
		descriptor, _ := descriptorByKind(record.EventKind)
		batch = append(batch, RecordDraft{descriptor: descriptor, turnID: turn, payload: record.Payload})
		if record.BatchIndex+1 == record.BatchSize {
			if _, err := writer.AppendBatch(context.Background(), batch); err != nil {
				t.Fatal(err)
			}
			batch = batch[:0]
		}
	}
	appendHistoricalFailure(t, writer, turn)
	return turn
}

func appendHistoricalFailure(t *testing.T, writer *JournalWriter, turn domain.TurnID) {
	t.Helper()
	failed := mustDraft(t, EventTurnFailed, turn, TurnFailedPayload{Code: "user_cancelled", Message: "User cancelled"})
	if _, err := writer.AppendBatch(context.Background(), []RecordDraft{failed}); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalSourceReadWaitsForOwnerAndRejectsPhysicalTail(t *testing.T) {
	ctx := context.Background()
	repo := openBranchTestRepository(t, securePathHooks{})
	content := readCurrentFixture(t, "root.jsonl")
	lease := installBranchJournal(t, repo, testThreadID, content)
	loaded := loadFixtureJournal(t, ctx, lease)
	file, err := lease.transfer()
	requireNoError(t, err)
	gated := &historicalReadGate{File: file, entered: make(chan struct{}), release: make(chan struct{})}
	writer := startOwnedJournalWriter(gated, loaded.Identity, loaded.NextSequence, loaded.committedBytes, time.Now, journalRequestCapacity)
	release := sync.OnceFunc(func() { close(gated.release) })
	t.Cleanup(func() {
		release()
		_ = writer.Close(ctx)
	})
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := writer.ReadHistorySnapshot(readCtx)
		result <- err
	}()
	<-gated.entered
	cancel()
	select {
	case err := <-result:
		t.Fatalf("read returned before descriptor owner completed: %v", err)
	default:
	}
	release()
	if err := <-result; !errors.Is(err, context.Canceled) || writer.Poisoned() {
		t.Fatalf("cancelled source read = error %v, poison %t", err, writer.Poisoned())
	}
	if cuts, _ := readHistoricalCuts(t, writer); len(cuts) != 0 {
		t.Fatalf("only failed-before-input and first root input turns, got %d candidates", len(cuts))
	}
	tail := []byte("uncommitted tail")
	filename, err := repo.JournalPath(testThreadID)
	requireNoError(t, err)
	extra, err := os.OpenFile(filename, os.O_WRONLY|os.O_APPEND, 0)
	requireNoError(t, err)
	_, writeErr := extra.Write(tail)
	if err := errors.Join(writeErr, extra.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ReadHistorySnapshot(ctx); !IsBranchSourceUnsafe(err) || !writer.Poisoned() {
		t.Fatalf("read across physical tail = error %v, poison %t", err, writer.Poisoned())
	}
	if actual := readBranchTestJournal(t, repo, testThreadID); !bytes.Equal(actual, append(bytes.Clone(content), tail...)) {
		t.Fatal("cancelled or rejected read changed journal bytes")
	}
}

type historicalReadGate struct {
	*os.File
	entered, release chan struct{}
	once             sync.Once
}

type historicalListFixtureFile struct{ *os.File }

func (historicalListFixtureFile) Sync() error { return nil }

func (file *historicalReadGate) ReadAt(data []byte, offset int64) (int, error) {
	file.once.Do(func() {
		close(file.entered)
		<-file.release
	})
	return file.File.ReadAt(data, offset)
}

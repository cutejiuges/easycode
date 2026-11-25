package session

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
)

const journalLeaseHelperEnv = "EASYCODE_TEST_JOURNAL_LEASE_HELPER"

func TestJournalLeaseStateTransitionsAndIndependentRepositories(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	first := openFixtureRepository(t, dataRoot)
	defer first.Close()
	second := openFixtureRepository(t, dataRoot)
	defer second.Close()

	lease, err := first.Create(context.Background(), testThreadID)
	requireNoError(t, err)
	path, err := first.JournalPath(testThreadID)
	requireNoError(t, err)
	before := readSessionFixtureFile(t, path)
	if _, err := second.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("second repository Open() error = %v", err)
	}
	after := readSessionFixtureFile(t, path)
	if !bytes.Equal(after, before) {
		t.Fatal("failed lease acquisition modified the journal")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("second lease Close() error = %v", err)
	}
	reacquired, err := second.Open(context.Background(), testThreadID)
	requireNoError(t, err)
	if err := reacquired.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJournalLeaseProcessOwnershipAndCrashRelease(t *testing.T) {
	dataRoot := createLeaseProcessJournal(t, []byte("fixed-prefix\n"))
	competitor := openFixtureRepository(t, dataRoot)
	defer competitor.Close()
	path, err := competitor.JournalPath(testThreadID)
	requireNoError(t, err)

	t.Run("normal close", func(t *testing.T) {
		helper := startJournalLeaseHelper(t, dataRoot)
		before := readSessionFixtureFile(t, path)
		if _, err := competitor.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
			t.Fatalf("competing Open() error = %v", err)
		}
		after := readSessionFixtureFile(t, path)
		if !bytes.Equal(after, before) {
			t.Fatal("busy acquisition modified journal bytes")
		}
		helper.stop(t)
		lease, err := competitor.Open(context.Background(), testThreadID)
		requireNoError(t, err)
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("forced termination", func(t *testing.T) {
		helper := startJournalLeaseHelper(t, dataRoot)
		helper.kill(t)
		lease, err := competitor.Open(context.Background(), testThreadID)
		requireNoError(t, err)
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLoaderDoesNotRepairWhileHelperOwnsLease(t *testing.T) {
	committed := encodeTestBatch(t, 1, []RecordDraft{
		mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}),
	})
	torn := append(append([]byte(nil), committed...), []byte(`{"seq":1,"timestamp"`)...)
	dataRoot := createLeaseProcessJournal(t, torn)
	repository := openFixtureRepository(t, dataRoot)
	defer repository.Close()
	path, err := repository.JournalPath(testThreadID)
	requireNoError(t, err)
	helper := startJournalLeaseHelper(t, dataRoot)
	if _, err := repository.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("Open() while helper owns lease error = %v", err)
	}
	during := readSessionFixtureFile(t, path)
	if !bytes.Equal(during, torn) {
		t.Fatal("busy load repaired the journal")
	}
	helper.stop(t)

	lease, err := repository.Open(context.Background(), testThreadID)
	requireNoError(t, err)
	first := loadFixtureJournal(t, context.Background(), lease)
	if !first.NeedsRepair() {
		t.Fatal("expected pending repair")
	}
	first.Repair, err = NewLoader().Repair(context.Background(), lease, first)
	requireNoError(t, err)
	if !first.Repair.Repaired || first.Repair.Kind != RepairHalfLine {
		t.Fatalf("first repair = %#v", first.Repair)
	}
	second := loadFixtureJournal(t, context.Background(), lease)
	if second.Repair.Repaired {
		t.Fatalf("second repair = %#v", second.Repair)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLoaderErrorLeavesLeaseWithCaller(t *testing.T) {
	dataRoot := createLeaseProcessJournal(t, []byte("{invalid json}\n"))
	owner := openFixtureRepository(t, dataRoot)
	defer owner.Close()
	competitor := openFixtureRepository(t, dataRoot)
	defer competitor.Close()
	lease, err := owner.Open(context.Background(), testThreadID)
	requireNoError(t, err)
	if _, err := NewLoader().Load(context.Background(), lease); err == nil {
		t.Fatal("Load() unexpectedly accepted corrupt journal")
	}
	if _, err := competitor.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("load error released caller lease, competing error = %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	reacquired, err := competitor.Open(context.Background(), testThreadID)
	requireNoError(t, err)
	if err := reacquired.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJournalLeaseHelperProcess(t *testing.T) {
	if os.Getenv(journalLeaseHelperEnv) != "1" {
		return
	}
	dataRoot := os.Getenv("EASYCODE_TEST_JOURNAL_ROOT")
	threadID, err := domain.ParseThreadID(os.Getenv("EASYCODE_TEST_JOURNAL_THREAD"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	repository, err := OpenOrCreateRepository(dataRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	lease, err := repository.Open(context.Background(), threadID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if err := lease.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := repository.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

type journalLeaseHelper struct {
	command *exec.Cmd
	control io.WriteCloser
}

func startJournalLeaseHelper(t *testing.T, dataRoot string) *journalLeaseHelper {
	t.Helper()
	return startJournalLeaseHelperForThread(t, dataRoot, testThreadID)
}

func startJournalLeaseHelperForThread(t *testing.T, dataRoot string, threadID domain.ThreadID) *journalLeaseHelper {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestJournalLeaseHelperProcess$")
	command.Env = append(os.Environ(),
		journalLeaseHelperEnv+"=1",
		"EASYCODE_TEST_JOURNAL_ROOT="+dataRoot,
		"EASYCODE_TEST_JOURNAL_THREAD="+string(threadID),
	)
	stdout, err := command.StdoutPipe()
	requireNoError(t, err)
	control, err := command.StdinPipe()
	requireNoError(t, err)
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "ready\n" {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("helper ready = %q, %v, stderr=%s", ready, err, stderr.String())
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = control.Close()
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	return &journalLeaseHelper{command: command, control: control}
}

func (helper *journalLeaseHelper) stop(t *testing.T) {
	t.Helper()
	if err := helper.control.Close(); err != nil {
		t.Fatal(err)
	}
	if err := helper.command.Wait(); err != nil {
		t.Fatal(err)
	}
}

func (helper *journalLeaseHelper) kill(t *testing.T) {
	t.Helper()
	if err := helper.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.control.Close()
	if err := helper.command.Wait(); err == nil {
		t.Fatal("killed helper exited successfully")
	}
}

func createLeaseProcessJournal(t *testing.T, content []byte) string {
	t.Helper()
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	repository := openFixtureRepository(t, dataRoot)
	lease, err := repository.Create(context.Background(), testThreadID)
	requireNoError(t, err)
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := repository.JournalPath(testThreadID)
	requireNoError(t, err)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	return dataRoot
}

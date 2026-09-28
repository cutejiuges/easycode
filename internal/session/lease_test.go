package session

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

func TestJournalLockDistinguishesBusyFromSystemError(t *testing.T) {
	repository, err := NewRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	owner, err := repository.Create(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	competitor, err := NewRepository(repository.RootPath())
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.Close()
	if _, err := competitor.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("competing Open() error = %v", err)
	}

	closed, err := os.OpenFile(filepath.Join(t.TempDir(), "closed"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tryLockJournal(closed); err == nil || IsJournalBusy(err) {
		t.Fatalf("tryLockJournal(closed) error = %v", err)
	}
}

func TestJournalLeaseStateTransitionsAndIndependentRepositories(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	first, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	lease, err := first.Create(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	path, err := first.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("second repository Open() error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	if err := reacquired.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJournalLeaseRejectsIllegalTransfer(t *testing.T) {
	repository, err := NewRepository(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	lease, err := repository.Create(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := lease.transfer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.transfer(); !errors.Is(err, errJournalLeaseTransferred) {
		t.Fatalf("second transfer error = %v", err)
	}
	if err := lease.Close(); !errors.Is(err, errJournalLeaseTransferred) {
		t.Fatalf("transferred lease Close() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestJournalLeaseProcessOwnershipAndCrashRelease(t *testing.T) {
	dataRoot := createLeaseProcessJournal(t, []byte("fixed-prefix\n"))
	competitor, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.Close()
	path, err := competitor.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("normal close", func(t *testing.T) {
		helper := startJournalLeaseHelper(t, dataRoot)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := competitor.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
			t.Fatalf("competing Open() error = %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("busy acquisition modified journal bytes")
		}
		helper.stop(t)
		lease, err := competitor.Open(context.Background(), testThreadID)
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("forced termination", func(t *testing.T) {
		helper := startJournalLeaseHelper(t, dataRoot)
		helper.kill(t)
		lease, err := competitor.Open(context.Background(), testThreadID)
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLoaderDoesNotRepairWhileHelperOwnsLease(t *testing.T) {
	committed := encodeTestBatch(t, 1, []RecordDraft{{
		EventKind: EventTurnStarted, TurnID: testTurnID, Payload: TurnStartedPayload{},
	}})
	torn := append(append([]byte(nil), committed...), []byte(`{"schema_version":1,"payload_version"`)...)
	dataRoot := createLeaseProcessJournal(t, torn)
	repository, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	helper := startJournalLeaseHelper(t, dataRoot)
	if _, err := repository.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("Open() while helper owns lease error = %v", err)
	}
	during, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(during, torn) {
		t.Fatal("busy load repaired the journal")
	}
	helper.stop(t)

	lease, err := repository.Open(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Repair.Repaired || first.Repair.Kind != RepairHalfLine {
		t.Fatalf("first repair = %#v", first.Repair)
	}
	second, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if second.Repair.Repaired {
		t.Fatalf("second repair = %#v", second.Repair)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLoaderErrorLeavesLeaseWithCaller(t *testing.T) {
	dataRoot := createLeaseProcessJournal(t, []byte("{invalid json}\n"))
	owner, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	competitor, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.Close()
	lease, err := owner.Open(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
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
	repository, err := NewRepository(dataRoot)
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
	command := exec.Command(os.Args[0], "-test.run=^TestJournalLeaseHelperProcess$")
	command.Env = append(os.Environ(),
		journalLeaseHelperEnv+"=1",
		"EASYCODE_TEST_JOURNAL_ROOT="+dataRoot,
		"EASYCODE_TEST_JOURNAL_THREAD="+string(testThreadID),
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	control, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
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
	repository, err := NewRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repository.Create(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	return dataRoot
}

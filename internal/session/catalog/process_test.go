//go:build darwin || linux

package catalog

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/session"
)

const (
	catalogLockHelperEnv    = "EASYCODE_TEST_CATALOG_LOCK_HELPER"
	catalogJournalHelperEnv = "EASYCODE_TEST_CATALOG_JOURNAL_HELPER"
)

func TestCatalogLockAcrossProcessesAndCrashRelease(t *testing.T) {
	directory := privateTempDir(t)
	databasePath := filepath.Join(directory, "state.sqlite")
	root := filepath.Join(directory, "sessions")
	helper := startCatalogProcessHelper(t, catalogLockHelperEnv, databasePath, root, "")

	competitor, err := New(Config{DatabasePath: databasePath, SessionRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := competitor.Open(ctx); err == nil || ctx.Err() == nil {
		t.Fatalf("competing Open() error = %v, context = %v", err, ctx.Err())
	}
	helper.kill(t)
	if err := competitor.Open(context.Background()); err != nil {
		t.Fatalf("Open() after owner crash: %v", err)
	}
	if err := competitor.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilePreservesBusyRowsAndRejectsBusyUnindexed(t *testing.T) {
	directory := privateTempDir(t)
	root := filepath.Join(directory, "sessions")
	databasePath := filepath.Join(directory, "state.sqlite")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	cwd := t.TempDir()
	first := createCatalogJournal(t, repository, cwd)
	instance := openTestCatalog(t, databasePath, root)
	defer func() { _ = instance.Close() }()
	if _, err := instance.Reconcile(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	firstHelper := startCatalogProcessHelper(t, catalogJournalHelperEnv, "", root, string(first))
	report, err := instance.Reconcile(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if report.Busy != 1 || report.BusyUnindexed {
		t.Fatalf("indexed busy report = %#v", report)
	}
	firstHelper.stop(t)

	second := createCatalogJournal(t, repository, cwd)
	path, err := repository.JournalPath(second)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	secondHelper := startCatalogProcessHelper(t, catalogJournalHelperEnv, "", root, string(second))
	report, err = instance.Reconcile(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if report.Busy != 1 || !report.BusyUnindexed {
		t.Fatalf("unindexed busy report = %#v", report)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("busy journal changed: %v", err)
	}
	secondHelper.kill(t)
	report, err = instance.Reconcile(context.Background(), repository)
	if err != nil || report.Valid != 2 || report.Busy != 0 {
		t.Fatalf("post-crash report = %#v, %v", report, err)
	}
}

func TestCatalogLockHelperProcess(t *testing.T) {
	if os.Getenv(catalogLockHelperEnv) != "1" {
		return
	}
	instance, err := New(Config{
		DatabasePath: os.Getenv("EASYCODE_TEST_CATALOG_DATABASE"),
		SessionRoot:  os.Getenv("EASYCODE_TEST_CATALOG_ROOT"),
	})
	if err == nil {
		err = instance.Open(context.Background())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if err := instance.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func TestCatalogJournalHelperProcess(t *testing.T) {
	if os.Getenv(catalogJournalHelperEnv) != "1" {
		return
	}
	threadID, err := domain.ParseThreadID(os.Getenv("EASYCODE_TEST_CATALOG_THREAD"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	repository, err := session.OpenOrCreateRepository(os.Getenv("EASYCODE_TEST_CATALOG_ROOT"))
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

type catalogProcessHelper struct {
	command *exec.Cmd
	control io.WriteCloser
}

func startCatalogProcessHelper(t *testing.T, helperEnv string, databasePath string, root string, threadID string) *catalogProcessHelper {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestCatalog(Lock|Journal)HelperProcess$")
	command.Env = append(os.Environ(),
		helperEnv+"=1",
		"EASYCODE_TEST_CATALOG_DATABASE="+databasePath,
		"EASYCODE_TEST_CATALOG_ROOT="+root,
		"EASYCODE_TEST_CATALOG_THREAD="+threadID,
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
		t.Fatalf("catalog helper ready = %q, %v, stderr=%s", ready, err, stderr.String())
	}
	helper := &catalogProcessHelper{command: command, control: control}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = control.Close()
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	return helper
}

func (helper *catalogProcessHelper) stop(t *testing.T) {
	t.Helper()
	if err := helper.control.Close(); err != nil {
		t.Fatal(err)
	}
	if err := helper.command.Wait(); err != nil {
		t.Fatal(err)
	}
}

func (helper *catalogProcessHelper) kill(t *testing.T) {
	t.Helper()
	if err := helper.command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := helper.command.Wait(); err == nil {
		t.Fatal("killed catalog helper exited successfully")
	}
}

func createCatalogJournal(t *testing.T, repository *session.Repository, cwd string) domain.ThreadID {
	t.Helper()
	identity, err := session.GenerateRootIdentity()
	if err != nil {
		t.Fatal(err)
	}
	writer, _, err := session.CreateRootJournal(context.Background(), repository, identity, session.RootJournalConfig{
		Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "gpt-test",
		CreationCWD: cwd, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	return identity.ThreadID
}

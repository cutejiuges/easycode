package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/session"
)

const appSessionHelperEnv = "EASYCODE_TEST_APP_SESSION_HELPER"

func TestSessionServiceResumeAcrossProcesses(t *testing.T) {
	owner := newTestSessionService(t)
	defer owner.close()
	created, err := owner.create(
		context.Background(), &fakeProviderFactory{family: domain.ProviderOpenAI},
		"responses", "gpt-test", t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := created.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	path, err := owner.repository.JournalPath(created.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	helper := startAppSessionHelper(t, owner.repository.RootPath(), created.identity.ThreadID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	competitor, err := openSessionService(owner.repository.RootPath())
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.close()
	factory := &fakeProviderFactory{family: domain.ProviderOpenAI}
	if _, err := competitor.resume(
		context.Background(), factory, "responses", "gpt-test", created.identity.ThreadID,
	); !errors.Is(err, &fault.Error{Code: fault.CodeSessionBusy}) {
		t.Fatalf("second process resume error = %v", err)
	}
	if factory.restoreCalls.Load() != 0 || factory.networkCalls.Load() != 0 {
		t.Fatalf("busy provider calls restore/network = %d/%d", factory.restoreCalls.Load(), factory.networkCalls.Load())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("cross-process busy resume modified journal bytes")
	}
	helper.stop(t)

	resumed, err := competitor.resume(
		context.Background(), factory, "responses", "gpt-test", created.identity.ThreadID,
	)
	if err != nil {
		t.Fatal(err)
	}
	turnID, err := domain.GenerateTurnID()
	if err != nil {
		t.Fatal(err)
	}
	startedDraft, err := session.NewTurnStartedDraft(turnID)
	if err != nil {
		t.Fatal(err)
	}
	records, err := resumed.writer.AppendBatch(context.Background(), []session.RecordDraft{startedDraft})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Sequence != 3 {
		t.Fatalf("resumed append records = %#v", records)
	}
	if err := resumed.writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, _ := loadAppPlan(t, competitor, created.identity.ThreadID)
	if loaded.NextSequence != 4 || loaded.Records[len(loaded.Records)-1].Sequence != 3 {
		t.Fatalf("resumed journal next/last = %d/%d", loaded.NextSequence, loaded.Records[len(loaded.Records)-1].Sequence)
	}
}

func TestAppSessionHelperProcess(t *testing.T) {
	if os.Getenv(appSessionHelperEnv) != "1" {
		return
	}
	threadID, err := domain.ParseThreadID(os.Getenv("EASYCODE_TEST_APP_SESSION_THREAD"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	service, err := openSessionService(os.Getenv("EASYCODE_TEST_APP_SESSION_ROOT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	resumed, err := service.resume(
		context.Background(), &fakeProviderFactory{family: domain.ProviderOpenAI},
		"responses", "gpt-test", threadID,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if err := resumed.writer.Close(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := service.close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

type appSessionHelper struct {
	command *exec.Cmd
	control io.WriteCloser
}

func startAppSessionHelper(t *testing.T, dataRoot string, threadID domain.ThreadID) *appSessionHelper {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestAppSessionHelperProcess$")
	command.Env = append(os.Environ(),
		appSessionHelperEnv+"=1",
		"EASYCODE_TEST_APP_SESSION_ROOT="+dataRoot,
		"EASYCODE_TEST_APP_SESSION_THREAD="+string(threadID),
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
		t.Fatalf("app helper ready = %q, %v, stderr=%s", ready, err, stderr.String())
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = control.Close()
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	return &appSessionHelper{command: command, control: control}
}

func (helper *appSessionHelper) stop(t *testing.T) {
	t.Helper()
	if err := helper.control.Close(); err != nil {
		t.Fatal(err)
	}
	if err := helper.command.Wait(); err != nil {
		t.Fatal(err)
	}
}

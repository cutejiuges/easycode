package app

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tui"
)

func TestPreviewLeaseProcess(t *testing.T) {
	if os.Getenv("EASYCODE_PREVIEW_LEASE_CHILD") != "1" {
		return
	}
	root := os.Getenv("EASYCODE_PREVIEW_ROOT")
	thread, err := domain.ParseThreadID(os.Getenv("EASYCODE_PREVIEW_THREAD"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Open(context.Background(), thread)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	fmt.Fprintln(os.Stdout, "ready")
	io.Copy(io.Discard, os.Stdin)
}
func TestPreviewBusyProcessCrashAndNoRepair(t *testing.T) {
	original, err := os.ReadFile("../session/testdata/current/input_only_openai.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	data := append(append([]byte(nil), original...), []byte(`{"partial":`)...)
	root, path, thread := installPreviewFixture(t, data)
	command := exec.Command(os.Args[0], "-test.run=^TestPreviewLeaseProcess$")
	command.Env = append(os.Environ(), "EASYCODE_PREVIEW_LEASE_CHILD=1", "EASYCODE_PREVIEW_ROOT="+root, "EASYCODE_PREVIEW_THREAD="+string(thread))
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	control, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		control.Close()
		if command.ProcessState == nil {
			command.Process.Kill()
			command.Wait()
		}
	})
	ready, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatalf("child: %v %s", err, stderr.String())
	}
	result, cleanup, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
	if err == nil || cleanup || len(result.Messages) != 0 {
		t.Fatal("busy preview succeeded")
	}
	execute := newPickerExecutor(config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: "https://example.invalid", APIKey: secret.New("fixture")}}, dataPaths{sessionRoot: root}, t.TempDir())
	rejected := execute(context.Background(), tui.PickerRequest{ID: 1, Operation: tui.PickerResume, ThreadID: thread})
	if !strings.Contains(rejected.result.Error, "session_busy") || rejected.result.Fatal || rejected.resources != nil {
		t.Fatal("busy recovery was not rejected before mutation")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, data) {
		t.Fatal("busy preview repaired journal")
	}
	if err = command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if command.Wait() == nil {
		t.Fatal("child did not crash")
	}
	result, cleanup, err = previewSession(context.Background(), root, thread, defaultPreviewDependencies())
	if err != nil || cleanup || !result.Incomplete {
		t.Fatalf("crash lease release failed: %v", err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(after, data) {
		t.Fatal("read-only preview repaired journal")
	}
	recovered := execute(context.Background(), tui.PickerRequest{ID: 2, Operation: tui.PickerResume, ThreadID: thread})
	if recovered.result.Error != "" || recovered.resources == nil {
		t.Fatal("recovery after crash failed", recovered.result.Error)
	}
	if err := closePickerResources(recovered.resources); err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(committed, original) || bytes.Equal(committed, original) {
		t.Fatal("recovery did not preserve prefix and append compensation")
	}
	for i, line := range bytes.Split(bytes.TrimSuffix(committed, []byte{'\n'}), []byte{'\n'}) {
		record, err := session.DecodeRecord(line)
		if err != nil || record.Sequence != uint64(i+1) {
			t.Fatal("post-crash sequence is not monotonic", err)
		}
	}

}

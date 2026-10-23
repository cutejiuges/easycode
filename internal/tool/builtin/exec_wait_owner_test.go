package builtin

import (
	"context"
	"io"
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestProcessExecutionKeepsUniqueReaperUntilFinalWaitCompletes(t *testing.T) {
	release, entered := make(chan struct{}), make(chan struct{})
	process := &fakeOwnedProcess{out: io.NopCloser(strings.NewReader("output")), errout: io.NopCloser(strings.NewReader("")), waitResult: make(chan processWaitResult, 1), waitEntered: entered, waitRelease: release}
	process.waitResult <- processWaitResult{exit: 0}
	execution, err := newProcessExecution(&fakeProcessLauncher{process: process}, wallProcessClock{}, processTestCandidate(t), tool.RenderExecOutput)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan tool.InvocationResult, 1)
	go func() { finished <- execution.Run(ctx) }()
	<-entered
	cancel()
	select {
	case <-execution.Done():
		t.Fatal("unreaped process released owner")
	default:
	}
	select {
	case <-finished:
		t.Fatal("unreaped process published result")
	default:
	}
	if result := execution.Run(context.Background()); result.Code() != "execution_already_accepted" {
		t.Fatal("second reaper accepted")
	}
	close(release)
	if result := <-finished; result.Status() != tool.ResultSuccess {
		t.Fatalf("known exit lost during cleanup: %s", result.Code())
	}
	<-execution.Done()
	if process.waits != 1 || process.staleOperations != 0 {
		t.Fatal("reaper reused process identity")
	}
}

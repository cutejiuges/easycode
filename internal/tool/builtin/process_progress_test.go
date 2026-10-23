package builtin

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"easycode/internal/tool"
)

func TestProcessProgressCoalescesWithoutBlocking(t *testing.T) {
	progress := newProcessProgress()
	var workers sync.WaitGroup
	workers.Add(2)
	for _, stderr := range []bool{false, true} {
		go func() {
			defer workers.Done()
			for range 1000 {
				progress.update([]byte("你好\n"), stderr, 12)
			}
		}()
	}
	workers.Wait()
	snapshot := progress.snapshot()
	if snapshot.Validate() != nil || snapshot.StdoutBytes() != 7000 || snapshot.StderrBytes() != 7000 || snapshot.StdoutLines() != 1000 || snapshot.StderrLines() != 1000 {
		t.Fatalf("invalid merged counters: %#v", snapshot)
	}
	if len(progress.notify) != 1 {
		t.Fatal("notifications are not coalesced")
	}
	progress.update(bytes.Repeat([]byte("x"), tool.ExecCaptureBytesPerStream+1), false, 1)
	if !progress.snapshot().Truncated() || progress.snapshot().ElapsedMillis() != 12 || snapshot.Truncated() {
		t.Fatal("snapshot changed or counters regressed")
	}
}

func TestProcessExecutionFinalProgressBeforeDone(t *testing.T) {
	process := &fakeOwnedProcess{out: io.NopCloser(strings.NewReader("output\n")), errout: io.NopCloser(strings.NewReader("error\n")), waitResult: make(chan processWaitResult, 1)}
	process.waitResult <- processWaitResult{}
	execution, _ := newProcessExecution(&fakeProcessLauncher{process: process}, wallProcessClock{}, processTestCandidate(t), tool.RenderExecOutput)
	result := execution.Run(context.Background())
	if result.Status() != tool.ResultSuccess {
		t.Fatal(result.Code())
	}
	<-execution.Done()
	snapshot := execution.Progress()
	if snapshot.StdoutBytes() != 7 || snapshot.StderrBytes() != 6 || snapshot.StdoutLines() != 1 || snapshot.StderrLines() != 1 {
		t.Fatal("final progress missing")
	}
	for range execution.Notifications() {
	}
	if execution.Progress() != snapshot {
		t.Fatal("progress changed after cleanup")
	}
}

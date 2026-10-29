package builtin

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"easycode/internal/tool"
)

type blockedOutputCapture struct {
	mode                     string
	entered, release, closed chan struct{}
}

func TestArtifactBlockedWriteKeepsOwnerAfterTimeoutKill(t *testing.T) {
	capture := &blockedOutputCapture{mode: "write", entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	process := &fakeOwnedProcess{out: io.NopCloser(strings.NewReader("output")), errout: io.NopCloser(strings.NewReader("")), alive: true, waitResult: make(chan processWaitResult, 1)}
	clock := &fakeProcessClock{timers: make(chan *fakeProcessTimer, 20)}
	execution, err := newProcessExecution(&fakeProcessLauncher{process: process}, clock, processTestCandidate(t), tool.RenderExecOutput)
	if err != nil {
		t.Fatal(err)
	}
	execution.outputStore = ownedOutputStore{capture}
	done := make(chan tool.InvocationResult, 1)
	go func() { done <- execution.Run(context.Background()) }()
	<-capture.entered
	timeout := <-clock.timers
	timeout.ticks <- time.Unix(1, 0)
	grace := <-clock.timers
	if grace.duration != 250*time.Millisecond {
		t.Fatal("missing TERM grace")
	}
	grace.ticks <- time.Unix(1, 0)
	drain := <-clock.timers
	if drain.duration != time.Second {
		t.Fatal("missing output drain deadline")
	}
	select {
	case <-execution.Done():
		t.Fatal("timeout kill abandoned blocked disk write")
	default:
	}
	select {
	case <-capture.closed:
		t.Fatal("timeout kill released capture")
	default:
	}
	close(capture.release)
	result := <-done
	if result.Code() != "timed_out" || !result.ExecMetadata().TimedOut() || process.waits != 1 {
		t.Fatalf("timeout result: %s", result.Code())
	}
	<-execution.Done()
	<-capture.closed
}

func (c *blockedOutputCapture) WriteStdout(p []byte) (int, error) {
	if c.mode == "write" {
		close(c.entered)
		<-c.release
	}
	return len(p), nil
}
func (*blockedOutputCapture) WriteStderr(p []byte) (int, error) { return len(p), nil }
func (c *blockedOutputCapture) Finish(bool, bool, bool) (tool.ArtifactSaving, tool.ArtifactSaving) {
	if c.mode == "seal" {
		close(c.entered)
		<-c.release
	}
	v, _ := tool.NewArtifactSaving(tool.ArtifactNotNeeded, "", tool.ArtifactDescriptor{})
	return v, v
}
func (c *blockedOutputCapture) Close() error { close(c.closed); return nil }

type ownedOutputStore struct{ capture tool.CommandOutputCapture }

func (s ownedOutputStore) StartCapture(context.Context, tool.InvocationID) (tool.CommandOutputCapture, error) {
	return s.capture, nil
}

func TestArtifactBlockingIOPreservesProcessCleanupOwner(t *testing.T) {
	for _, mode := range []string{"write", "seal"} {
		t.Run(mode, func(t *testing.T) {
			capture := &blockedOutputCapture{mode: mode, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
			process := &fakeOwnedProcess{out: io.NopCloser(strings.NewReader("output")), errout: io.NopCloser(strings.NewReader("")), waitResult: make(chan processWaitResult, 1)}
			process.waitResult <- processWaitResult{exit: 0}
			clock := &fakeProcessClock{timers: make(chan *fakeProcessTimer, 20)}
			execution, err := newProcessExecution(&fakeProcessLauncher{process: process}, clock, processTestCandidate(t), tool.RenderExecOutput)
			if err != nil {
				t.Fatal(err)
			}
			execution.outputStore = ownedOutputStore{capture}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan tool.InvocationResult, 1)
			go func() { finished <- execution.Run(ctx) }()
			<-capture.entered
			cancel()
			select {
			case <-execution.Done():
				t.Fatal("blocked I/O abandoned owner")
			default:
			}
			select {
			case <-capture.closed:
				t.Fatal("blocked I/O released capture")
			default:
			}
			if execution.Run(context.Background()).Code() != "execution_already_accepted" {
				t.Fatal("blocked owner accepted second execution")
			}
			close(capture.release)
			result := <-finished
			if result.Validate() != nil {
				t.Fatal("cleanup produced invalid result")
			}
			<-execution.Done()
			select {
			case <-capture.closed:
			default:
				t.Fatal("done preceded capture cleanup")
			}
			if process.waits != 1 {
				t.Fatal("process reaped more than once")
			}
		})
	}
}

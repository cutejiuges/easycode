package builtin

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/tool"
)

type fakeProcessTimer struct {
	ticks    chan time.Time
	duration time.Duration
}

func (timer *fakeProcessTimer) channel() <-chan time.Time { return timer.ticks }
func (*fakeProcessTimer) stop()                           {}

type fakeProcessClock struct{ timers chan *fakeProcessTimer }

func (*fakeProcessClock) now() time.Time { return time.Unix(0, 0) }
func (clock *fakeProcessClock) timer(duration time.Duration) processTimer {
	timer := &fakeProcessTimer{ticks: make(chan time.Time, 1), duration: duration}
	clock.timers <- timer
	return timer
}

type fakeOwnedProcess struct {
	mu              sync.Mutex
	out, errout     io.ReadCloser
	waitResult      chan processWaitResult
	alive           bool
	signals         []bool
	waits           int
	closeOutput     func()
	observedResult  processWaitResult
	reaped          bool
	staleOperations int
	probeError      error
	terminateError  error
	keepAlive       bool
	waitEntered     chan struct{}
	waitRelease     <-chan struct{}
}

func (process *fakeOwnedProcess) stdout() io.ReadCloser { return process.out }
func (process *fakeOwnedProcess) stderr() io.ReadCloser { return process.errout }
func (process *fakeOwnedProcess) observeExit() error {
	result := <-process.waitResult
	process.mu.Lock()
	process.observedResult = result
	process.mu.Unlock()
	return result.err
}
func (process *fakeOwnedProcess) wait() (int, error) {
	if process.waitEntered != nil {
		close(process.waitEntered)
	}
	if process.waitRelease != nil {
		<-process.waitRelease
	}
	process.mu.Lock()
	process.waits++
	process.reaped = true
	result := process.observedResult
	process.mu.Unlock()
	return result.exit, result.err
}
func (process *fakeOwnedProcess) groupAlive() (bool, error) {
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.reaped {
		process.staleOperations++
		return true, errors.New("reused process identity")
	}
	return process.alive, process.probeError
}
func (process *fakeOwnedProcess) terminate(force bool) error {
	process.mu.Lock()
	if process.reaped {
		process.staleOperations++
		process.mu.Unlock()
		return errors.New("reused process identity")
	}
	process.signals = append(process.signals, force)
	if force && !process.keepAlive {
		process.alive = false
	}
	process.mu.Unlock()
	if force {
		process.waitResult <- processWaitResult{exit: -1}
		if process.closeOutput != nil {
			process.closeOutput()
		}
	}
	return process.terminateError
}

type fakeProcessLauncher struct {
	process *fakeOwnedProcess
	calls   int
	err     error
	started chan struct{}
}

func (launcher *fakeProcessLauncher) start(context.Context, tool.ExecCandidate) (processHandle, error) {
	launcher.calls++
	if launcher.started != nil {
		close(launcher.started)
	}
	return launcher.process, launcher.err
}

func processTestCandidate(t *testing.T) tool.ExecCandidate {
	t.Helper()
	input, _ := tool.NewExecInput("printf hello", "", 1000)
	call, _ := tool.NewExecReadyCall("call_exec", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, call)
	shell, _ := tool.NewShellProfile("sh", "/bin/sh")
	environment, _ := tool.NewEnvironmentPolicy("minimal", []string{"PATH"})
	sandbox, _ := tool.NewSandboxProfile("workspace", false, true)
	candidate, err := tool.NewExecCandidate(invocation, shell, environment, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestProcessExecutionCompletesAndCannotRerun(t *testing.T) {
	process := &fakeOwnedProcess{out: io.NopCloser(strings.NewReader("hello")), errout: io.NopCloser(strings.NewReader("warning")), waitResult: make(chan processWaitResult, 1)}
	process.waitResult <- processWaitResult{exit: 2}
	launcher := &fakeProcessLauncher{process: process}
	renders := 0
	renderer := func(stdout, stderr tool.ExecOutputSnapshot, metadata tool.ExecResultMetadata) (tool.ModelPreview, bool, error) {
		renders++
		if stdout.Stats().ObservedBytes() != 5 || stderr.Stats().ObservedBytes() != 7 {
			t.Fatal("renderer did not receive captured streams")
		}
		return tool.RenderExecOutput(stdout, stderr, metadata)
	}
	execution, err := newProcessExecution(launcher, wallProcessClock{}, processTestCandidate(t), renderer)
	if err != nil || launcher.calls != 0 {
		t.Fatal("constructor launched process")
	}
	result := execution.Run(context.Background())
	if result.Status() != tool.ResultSuccess || !strings.Contains(result.Preview().Text(), "hello") || !strings.Contains(result.Preview().Text(), "warning") {
		t.Fatalf("unexpected result: %s %s", result.Code(), result.Preview().Text())
	}
	if exit, known := result.ExecMetadata().ExitCode(); !known || exit != 2 {
		t.Fatal("nonzero exit lost")
	}
	select {
	case <-execution.Done():
	default:
		t.Fatal("cleanup not complete")
	}
	if execution.Run(context.Background()).Code() != "execution_already_accepted" || launcher.calls != 1 || process.waits != 1 || renders != 1 {
		t.Fatal("execution repeated")
	}
}

func TestProcessExecutionCancellationAndTimeout(t *testing.T) {
	for _, cancelTurn := range []bool{false, true} {
		clock := &fakeProcessClock{timers: make(chan *fakeProcessTimer, 8)}
		out, outWriter := io.Pipe()
		errout, errWriter := io.Pipe()
		process := &fakeOwnedProcess{out: out, errout: errout, alive: true, waitResult: make(chan processWaitResult, 1), closeOutput: func() { _ = outWriter.Close(); _ = errWriter.Close() }}
		launcher := &fakeProcessLauncher{process: process, started: make(chan struct{})}
		execution, _ := newProcessExecution(launcher, clock, processTestCandidate(t), tool.RenderExecOutput)
		ctx, cancel := context.WithCancel(context.Background())
		resultDone := make(chan tool.InvocationResult, 1)
		go func() { resultDone <- execution.Run(ctx); close(resultDone) }()
		<-launcher.started
		timeout := <-clock.timers
		if cancelTurn {
			cancel()
		} else {
			timeout.ticks <- time.Unix(1, 0)
		}
		grace := <-clock.timers
		if grace.duration != 250*time.Millisecond {
			t.Fatal("missing TERM grace period")
		}
		select {
		case <-execution.Done():
			t.Fatal("owner abandoned live process")
		default:
		}
		grace.ticks <- time.Unix(1, 0)
		result := <-resultDone
		cancel()
		want := "timed_out"
		if cancelTurn {
			want = "cancelled"
		}
		if result.Code() != want || len(process.signals) != 2 || process.signals[0] || !process.signals[1] || process.waits != 1 || process.staleOperations != 0 {
			t.Fatalf("incorrect termination: %s %#v", result.Code(), process.signals)
		}
	}
}

func TestProcessExecutionDetachedWriterDrainDeadline(t *testing.T) {
	clock := &fakeProcessClock{timers: make(chan *fakeProcessTimer, 8)}
	out, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	process := &fakeOwnedProcess{out: out, errout: io.NopCloser(strings.NewReader("")), waitResult: make(chan processWaitResult, 1)}
	process.waitResult <- processWaitResult{exit: 0}
	execution, _ := newProcessExecution(&fakeProcessLauncher{process: process}, clock, processTestCandidate(t), tool.RenderExecOutput)
	resultDone := make(chan tool.InvocationResult, 1)
	go func() { resultDone <- execution.Run(context.Background()); close(resultDone) }()
	<-clock.timers
	drain := <-clock.timers
	drain.ticks <- time.Unix(1, 0)
	result := <-resultDone
	if result.Status() != tool.ResultOutcomeUncertain || result.Code() != "output_cleanup_incomplete" {
		t.Fatalf("detached writer misreported: %s", result.Code())
	}
	select {
	case <-execution.Done():
	default:
		t.Fatal("reader leaked")
	}
}

func TestProcessExecutionPrecancelAndLaunchFailure(t *testing.T) {
	for _, precancel := range []bool{false, true} {
		launcher := &fakeProcessLauncher{err: errors.New("private launch cause")}
		execution, _ := newProcessExecution(launcher, wallProcessClock{}, processTestCandidate(t), tool.RenderExecOutput)
		ctx, cancel := context.WithCancel(context.Background())
		if precancel {
			cancel()
		}
		result := execution.Run(ctx)
		cancel()
		if strings.Contains(result.Preview().Text(), "private") {
			t.Fatal("launch cause leaked")
		}
		if precancel && (launcher.calls != 0 || result.Status() != tool.ResultCancelled) {
			t.Fatal("pre-cancel started a process")
		}
	}
}

func TestProcessExecutionUnsupportedAndSandboxFailClosed(t *testing.T) {
	for _, test := range []struct {
		cause error
		code  string
	}{
		{errExecUnsupported, "unsupported_platform"},
		{errSandboxUnavailable, "sandbox_unavailable"},
	} {
		launcher := &fakeProcessLauncher{err: test.cause}
		execution, err := newProcessExecution(launcher, wallProcessClock{}, processTestCandidate(t), tool.RenderExecOutput)
		if err != nil {
			t.Fatal(err)
		}
		result := execution.Run(context.Background())
		if result.Status() != tool.ResultError || result.Code() != test.code || launcher.calls != 1 {
			t.Fatal("unavailable execution retried or lost safe error")
		}
		select {
		case <-execution.Done():
		default:
			t.Fatal("failed launch leaked owner")
		}
	}
}

func TestProcessExecutionRetainsIdentityUntilFinalWait(t *testing.T) {
	for _, failure := range []string{"none", "residual", "observer", "probe", "terminate", "group-still-alive"} {
		t.Run(failure, func(t *testing.T) {
			clock := &fakeProcessClock{timers: make(chan *fakeProcessTimer, 8)}
			process := &fakeOwnedProcess{out: io.NopCloser(strings.NewReader("")), errout: io.NopCloser(strings.NewReader("")), waitResult: make(chan processWaitResult, 1)}
			waitResult := processWaitResult{exit: 0}
			switch failure {
			case "residual":
				process.alive = true
			case "observer":
				waitResult.err = errors.New("observation unavailable")
			case "probe":
				process.probeError = errors.New("probe unavailable")
			case "terminate":
				process.alive = true
				process.terminateError = errors.New("termination uncertain")
			case "group-still-alive":
				process.alive, process.keepAlive = true, true
			}
			process.waitResult <- waitResult
			execution, err := newProcessExecution(&fakeProcessLauncher{process: process}, clock, processTestCandidate(t), tool.RenderExecOutput)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan tool.InvocationResult, 1)
			go func() { done <- execution.Run(context.Background()) }()
			var result tool.InvocationResult
		loop:
			for {
				select {
				case timer := <-clock.timers:
					if timer.duration == 250*time.Millisecond || timer.duration == 25*time.Millisecond {
						timer.ticks <- time.Unix(1, 0)
					}
				case result = <-done:
					break loop
				}
			}
			if process.waits != 1 || !process.reaped || process.staleOperations != 0 {
				t.Fatal("recycled process identity was used")
			}
			want := tool.ResultSuccess
			if failure != "none" && failure != "residual" {
				want = tool.ResultOutcomeUncertain
			}
			if result.Status() != want {
				t.Fatalf("result=%s code=%s", result.Status(), result.Code())
			}
		})
	}
}

func TestProcessExecutionDrainsBothStreamsBeforeWait(t *testing.T) {
	for _, stderrFirst := range []bool{false, true} {
		out, outWriter := io.Pipe()
		errout, errWriter := io.Pipe()
		process := &fakeOwnedProcess{out: out, errout: errout, waitResult: make(chan processWaitResult, 1)}
		execution, err := newProcessExecution(&fakeProcessLauncher{process: process}, wallProcessClock{}, processTestCandidate(t), tool.RenderExecOutput)
		if err != nil {
			t.Fatal(err)
		}
		writeDone := make(chan error, 1)
		// 两个零缓冲pipe只有reader真正并发排空才能让writer完成并触发Wait。
		go func() {
			first, second := outWriter, errWriter
			if stderrFirst {
				first, second = second, first
			}
			_, firstErr := io.WriteString(first, strings.Repeat("x", tool.ExecCaptureBytesPerStream+1024))
			_, secondErr := io.WriteString(second, strings.Repeat("x", tool.ExecCaptureBytesPerStream+1024))
			_ = outWriter.Close()
			_ = errWriter.Close()
			process.waitResult <- processWaitResult{exit: 0}
			writeDone <- errors.Join(firstErr, secondErr)
		}()
		result := execution.Run(context.Background())
		if err := <-writeDone; err != nil {
			t.Fatal(err)
		}
		if result.Status() != tool.ResultSuccess || !result.ExecMetadata().OutputTruncated() {
			t.Fatalf("large output did not drain: %s", result.Code())
		}
		for _, stats := range []tool.ExecStreamStats{result.ExecMetadata().Stdout(), result.ExecMetadata().Stderr()} {
			if stats.ObservedBytes() != tool.ExecCaptureBytesPerStream+1024 || stats.RetainedBytes() != tool.ExecCaptureBytesPerStream || stats.OmittedBytes() != 1024 {
				t.Fatal("large output accounting changed")
			}
		}
		metadata := result.ExecMetadata()
		var buffer tool.ExecOutputBuffer
		buffer.Write([]byte(strings.Repeat("x", tool.ExecCaptureBytesPerStream+1024)))
		preview, _, err := tool.RenderExecOutput(buffer.Snapshot(), buffer.Snapshot(), metadata)
		if err != nil || result.Preview().Text() != "Exec outcome: success (ok)\n"+preview.Text() {
			t.Fatal("stream scheduling changed durable preview")
		}
	}
}

package headless

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

type scriptedStreamLoop struct {
	mu       sync.Mutex
	closed   bool
	ready    chan struct{}
	done     chan struct{}
	outputs  chan protocol.ControlItem
	finish   chan struct{}
	once     sync.Once
	failTurn bool
}

func newScriptedStreamLoop(failTurn bool) *scriptedStreamLoop {
	return &scriptedStreamLoop{
		ready: make(chan struct{}), done: make(chan struct{}), outputs: make(chan protocol.ControlItem, 32),
		finish: make(chan struct{}), failTurn: failTurn,
	}
}

func (loop *scriptedStreamLoop) Run(ctx context.Context) error {
	close(loop.ready)
	select {
	case <-loop.finish:
	case <-ctx.Done():
	}
	loop.mu.Lock()
	loop.closed = true
	close(loop.outputs)
	loop.mu.Unlock()
	close(loop.done)
	return nil
}

func (loop *scriptedStreamLoop) Ready() <-chan struct{} { return loop.ready }
func (loop *scriptedStreamLoop) Done() <-chan struct{}  { return loop.done }
func (loop *scriptedStreamLoop) Outputs() <-chan protocol.ControlItem {
	return loop.outputs
}

func (loop *scriptedStreamLoop) Submit(_ context.Context, command protocol.Command) (protocol.CommandResult, error) {
	loop.mu.Lock()
	defer loop.mu.Unlock()
	if loop.closed {
		return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "scripted loop is closed")
	}
	disposition := protocol.CommandDispositionStarting
	if command.Kind() == protocol.CommandInterrupt {
		disposition = protocol.CommandDispositionInterrupting
	}
	if command.Kind() == protocol.CommandShutdown {
		disposition = protocol.CommandDispositionClosing
	}
	result, err := protocol.NewAcceptedCommandResult(command, disposition)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	item, _ := protocol.NewCommandResultItem(result)
	loop.outputs <- item
	if command.Kind() == protocol.CommandSubmitInput {
		loop.outputs <- mustScriptedTurnItem(command.RequestID(), protocol.NewTurnStarted())
		if loop.failTurn {
			failed, _ := protocol.NewTurnFailed(string(fault.CodeProviderRequest), "provider request failed", false)
			loop.outputs <- mustScriptedTurnItem(command.RequestID(), failed)
		} else {
			completed, _ := protocol.NewTurnCompleted(testHeadlessUsageForScript())
			loop.outputs <- mustScriptedTurnItem(command.RequestID(), completed)
		}
	}
	if command.Kind() == protocol.CommandShutdown {
		loop.stop()
	}
	return result, nil
}

func (loop *scriptedStreamLoop) CloseInput(context.Context) error {
	loop.stop()
	return nil
}

func (loop *scriptedStreamLoop) Stop(context.Context) error {
	loop.stop()
	<-loop.done
	return nil
}

func (loop *scriptedStreamLoop) stop() { loop.once.Do(func() { close(loop.finish) }) }

type bufferWriteCloser struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	closed chan struct{}
	wrote  chan struct{}
	once   sync.Once
}

func newBufferWriteCloser() *bufferWriteCloser {
	return &bufferWriteCloser{closed: make(chan struct{}), wrote: make(chan struct{}, 1)}
}

func (writer *bufferWriteCloser) Write(content []byte) (int, error) {
	writer.mu.Lock()
	written, err := writer.buffer.Write(content)
	writer.mu.Unlock()
	select {
	case writer.wrote <- struct{}{}:
	default:
	}
	return written, err
}

func (writer *bufferWriteCloser) String() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buffer.String()
}

func (writer *bufferWriteCloser) Close() error {
	writer.once.Do(func() { close(writer.closed) })
	return nil
}

type failAfterWriter struct {
	bytes.Buffer
	writes  int
	allowed int
	closed  chan struct{}
	once    sync.Once
}

func (writer *failAfterWriter) Write(content []byte) (int, error) {
	writer.writes++
	if writer.writes > writer.allowed {
		return 0, io.ErrClosedPipe
	}
	return writer.Buffer.Write(content)
}

func (writer *failAfterWriter) Close() error {
	writer.once.Do(func() { close(writer.closed) })
	return nil
}

type blockingWriteCloser struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

type earlyFailureStreamLoop struct {
	ready   chan struct{}
	done    chan struct{}
	outputs chan protocol.ControlItem
}

func newEarlyFailureStreamLoop() *earlyFailureStreamLoop {
	return &earlyFailureStreamLoop{
		ready: make(chan struct{}), done: make(chan struct{}), outputs: make(chan protocol.ControlItem),
	}
}

func (loop *earlyFailureStreamLoop) Run(context.Context) error {
	close(loop.outputs)
	close(loop.done)
	return fault.New(fault.CodeTurnFailed, "stream loop failed before ready")
}

func (loop *earlyFailureStreamLoop) Ready() <-chan struct{} { return loop.ready }
func (loop *earlyFailureStreamLoop) Done() <-chan struct{}  { return loop.done }
func (loop *earlyFailureStreamLoop) Outputs() <-chan protocol.ControlItem {
	return loop.outputs
}
func (*earlyFailureStreamLoop) Submit(context.Context, protocol.Command) (protocol.CommandResult, error) {
	return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "stream loop is unavailable")
}
func (*earlyFailureStreamLoop) CloseInput(context.Context) error { return nil }
func (*earlyFailureStreamLoop) Stop(context.Context) error       { return nil }

func (writer *blockingWriteCloser) Write([]byte) (int, error) {
	writer.once.Do(func() { close(writer.started) })
	<-writer.closed
	return 0, io.ErrClosedPipe
}

func (writer *blockingWriteCloser) Close() error {
	select {
	case <-writer.closed:
	default:
		close(writer.closed)
	}
	return nil
}

func TestStreamRunnerCleanEOFAndShutdownExitSuccessfully(t *testing.T) {
	for _, test := range []struct {
		name string
		wire string
	}{
		{name: "EOF drain", wire: `{"version":1,"type":"input.submit","request_id":"input-1","text":"hello"}` + "\n"},
		{name: "explicit shutdown without newline", wire: `{"version":1,"type":"session.shutdown","request_id":"shutdown-1"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputReader, inputWriter := io.Pipe()
			output := newBufferWriteCloser()
			loop := newScriptedStreamLoop(false)
			runner := newTestStreamRunner(t, loop, inputReader, output, output)
			writerDone := make(chan error, 1)
			go func() {
				_, err := io.WriteString(inputWriter, test.wire)
				if closeErr := inputWriter.Close(); err == nil {
					err = closeErr
				}
				writerDone <- err
			}()
			result := runner.Run(context.Background())
			if err := <-writerDone; err != nil {
				t.Fatal(err)
			}
			if !result.Completed || result.Failure.Code != "" {
				t.Fatalf("Run() result = %#v", result)
			}
			if !strings.Contains(output.String(), `"type":"thread.started"`) ||
				!strings.Contains(output.String(), `"type":"control.response"`) {
				t.Fatalf("stream output = %s", output.String())
			}
			if test.name == "EOF drain" && !strings.Contains(output.String(), `"type":"turn.completed"`) {
				t.Fatalf("EOF did not drain turn: %s", output.String())
			}
		})
	}
}

func TestStreamRunnerTurnFailureDoesNotPoisonCleanExit(t *testing.T) {
	input := io.NopCloser(strings.NewReader(`{"version":1,"type":"input.submit","request_id":"input-1","text":"hello"}`))
	output := newBufferWriteCloser()
	runner := newTestStreamRunner(t, newScriptedStreamLoop(true), input, output, output)
	result := runner.Run(context.Background())
	if !result.Completed || !strings.Contains(output.String(), `"type":"turn.failed"`) {
		t.Fatalf("Run() result/output = %#v/%s", result, output.String())
	}
}

func TestStreamRunnerMalformedInputReportsTerminalErrorAndCleansUp(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	output := newBufferWriteCloser()
	loop := newScriptedStreamLoop(false)
	runner := newTestStreamRunner(t, loop, inputReader, output, output)
	go func() {
		_, _ = io.WriteString(inputWriter, `{"version":1,"type":"input.submit","request_id":"input-1","text":"private","unknown":true}`+"\n")
		_ = inputWriter.Close()
	}()
	result := runner.Run(context.Background())
	if result.Failure.Code != fault.CodeStreamProtocol || !result.Reported || result.OutputUnavailable {
		t.Fatalf("Run() result = %#v", result)
	}
	if strings.Contains(output.String(), "private") || !strings.HasSuffix(output.String(), "\n") {
		t.Fatalf("unsafe malformed output = %q", output.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if !strings.Contains(lines[len(lines)-1], `"type":"error"`) {
		t.Fatalf("last output is not terminal error: %s", output.String())
	}
	<-loop.Done()
}

func TestStreamRunnerBrokenOutputStopsWithoutAppendingError(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	output := &failAfterWriter{allowed: 1, closed: make(chan struct{})}
	loop := newScriptedStreamLoop(false)
	runner := newTestStreamRunner(t, loop, inputReader, output, output)
	writerDone := make(chan error, 1)
	go func() {
		_, err := io.WriteString(inputWriter, `{"version":1,"type":"input.submit","request_id":"input-1","text":"hello"}`+"\n")
		if err == nil {
			_, err = io.WriteString(inputWriter, strings.Repeat("x", 1<<20))
		}
		writerDone <- err
	}()
	result := runner.Run(context.Background())
	if !result.OutputUnavailable || result.Failure.Code != fault.CodeOutput {
		t.Fatalf("Run() result = %#v", result)
	}
	if err := <-writerDone; err == nil {
		t.Fatal("input writer was not unblocked by transport close")
	}
	if output.writes != 2 {
		t.Fatalf("writer calls = %d, runner appended after failure", output.writes)
	}
	<-loop.Done()
}

func TestStreamRunnerCancellationUnblocksInitialOutputAndStdin(t *testing.T) {
	t.Run("blocked initial stdout", func(t *testing.T) {
		output := &blockingWriteCloser{started: make(chan struct{}), closed: make(chan struct{})}
		input := io.NopCloser(strings.NewReader(""))
		runner := newTestStreamRunner(t, newScriptedStreamLoop(false), input, output, output)
		ctx, cancel := context.WithCancel(context.Background())
		resultDone := make(chan Result, 1)
		go func() { resultDone <- runner.Run(ctx) }()
		<-output.started
		cancel()
		select {
		case result := <-resultDone:
			if !result.OutputUnavailable {
				t.Fatalf("Run() result = %#v", result)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("blocked stdout was not unblocked")
		}
	})

	t.Run("blocked stdin", func(t *testing.T) {
		inputReader, inputWriter := io.Pipe()
		defer inputWriter.Close()
		output := newBufferWriteCloser()
		loop := newScriptedStreamLoop(false)
		runner := newTestStreamRunner(t, loop, inputReader, output, output)
		ctx, cancel := context.WithCancel(context.Background())
		resultDone := make(chan Result, 1)
		go func() { resultDone <- runner.Run(ctx) }()
		waitForOutputContains(t, output, `"type":"thread.started"`)
		cancel()
		select {
		case result := <-resultDone:
			if result.Failure.Code == "" {
				t.Fatalf("Run() result = %#v", result)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("blocked stdin was not unblocked")
		}
		<-loop.Done()
	})
}

func TestStreamRunnerHandlesLoopFailureBeforeReady(t *testing.T) {
	t.Parallel()
	input := io.NopCloser(strings.NewReader(""))
	output := newBufferWriteCloser()
	loop := newEarlyFailureStreamLoop()
	runner := newTestStreamRunner(t, loop, input, output, output)
	result := runner.Run(context.Background())
	if result.Failure.Code != fault.CodeTurnFailed || !result.Reported || result.OutputUnavailable {
		t.Fatalf("Run() result = %#v", result)
	}
	if !strings.Contains(output.String(), `"type":"error"`) {
		t.Fatalf("stream output = %s", output.String())
	}
	<-loop.Done()
}

func TestStreamRunnerRunStateKeepsFirstFailure(t *testing.T) {
	t.Parallel()
	state := &streamRunnerRunState{loopFinished: true, readerFinished: true}
	first := fault.Summary{Code: fault.CodeStreamProtocol, Message: "first failure"}
	state.setFailure(first)
	state.setFailure(fault.Summary{Code: fault.CodeOutput, Message: "later failure"})
	if state.pendingFailure == nil || *state.pendingFailure != first {
		t.Fatalf("pending failure = %#v", state.pendingFailure)
	}
	if state.running() {
		t.Fatal("completed run state is still running")
	}
}

func newTestStreamRunner(
	t *testing.T,
	loop StreamControlLoop,
	input io.ReadCloser,
	output io.Writer,
	outputCloser io.Closer,
) *StreamRunner {
	t.Helper()
	runner, err := NewStreamRunner(loop, StreamRunConfig{
		SessionID: testSessionID, ThreadID: testThreadID,
		Input: input, Output: output, OutputCloser: outputCloser,
	})
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func mustScriptedTurnItem(requestID protocol.RequestID, event protocol.Event) protocol.ControlItem {
	event.SessionID, event.ThreadID, event.TurnID = testSessionID, testThreadID, testTurnID
	item, _ := protocol.NewCorrelatedTurnItem(requestID, event)
	return item
}

func testHeadlessUsageForScript() domain.SampleUsage {
	usage, _ := domain.NewSampleUsage(
		domain.KnownUsageMetric(0), domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric(),
		domain.KnownUsageMetric(1), domain.NotApplicableUsageMetric(),
	)
	return usage
}

func waitForOutputContains(t *testing.T, output *bufferWriteCloser, want string) {
	t.Helper()
	for {
		if strings.Contains(output.String(), want) {
			return
		}
		select {
		case <-output.wrote:
		case <-time.After(3 * time.Second):
			t.Fatalf("output never contained %q: %s", want, output.String())
		}
	}
}

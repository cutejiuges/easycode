package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/headless"
	"easycode/internal/protocol"
)

type effortStreamOutput struct {
	mu           sync.Mutex
	buffer       bytes.Buffer
	frames       chan []byte
	closed       chan struct{}
	once         sync.Once
	failSnapshot bool
}

func newEffortStreamOutput() *effortStreamOutput {
	return &effortStreamOutput{frames: make(chan []byte, 256), closed: make(chan struct{})}
}
func (w *effortStreamOutput) Write(raw []byte) (int, error) {
	if w.failSnapshot && bytes.Contains(raw, []byte(`"type":"effort.snapshot"`)) {
		w.frames <- append([]byte(nil), raw...)
		return min(7, len(raw)), io.ErrClosedPipe
	}
	w.mu.Lock()
	n, err := w.buffer.Write(raw)
	w.mu.Unlock()
	w.frames <- append([]byte(nil), raw...)
	return n, err
}
func (w *effortStreamOutput) Close() error { w.once.Do(func() { close(w.closed) }); return nil }
func effortFrame(t *testing.T, w *effortStreamOutput, contains string) []byte {
	t.Helper()
	for {
		select {
		case raw := <-w.frames:
			if bytes.Contains(raw, []byte(contains)) {
				return raw
			}
		case <-time.After(5 * time.Second):
			t.Fatal("missing stream frame", contains)
		}
	}
}

func TestEffortRealStreamRunnerWhileSamplingAndEOFDrain(t *testing.T) {
	c := newControlledConversation()
	j := &fakeJournal{}
	loop := effortLoop(t, c, j)
	input, send := io.Pipe()
	output := newEffortStreamOutput()
	runner, err := headless.NewStreamRunner(loop, headless.StreamRunConfig{SessionID: runtimeSessionID, ThreadID: runtimeThreadID, Input: input, Output: output, OutputCloser: output})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan headless.Result, 1)
	go func() { done <- runner.Run(context.Background()) }()
	effortFrame(t, output, `"type":"thread.started"`)
	if _, err := io.WriteString(send, "{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"work\",\"text\":\"/effort is literal text\"}\n"); err != nil {
		t.Fatal(err)
	}
	first := receiveInvocation(t, c.calls)
	if _, err := io.WriteString(send, "{\"version\":1,\"type\":\"effort.set\",\"request_id\":\"low\",\"mode\":\"value\",\"level\":\"low\"}\n"); err != nil {
		t.Fatal(err)
	}
	response := effortFrame(t, output, `"command_type":"effort.set"`)
	snapshot := effortFrame(t, output, `"type":"effort.snapshot"`)
	if !bytes.Contains(response, []byte(`"disposition":"updated"`)) || !bytes.Contains(snapshot, []byte(`"pending":true`)) || first.settings.Effort() != "high" || first.input != "/effort is literal text" {
		t.Fatal("active stream controls changed sampling", string(snapshot))
	}
	first.release <- completedProviderEvent(t)
	effortFrame(t, output, `"type":"turn.completed"`)
	if _, err := io.WriteString(send, "{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"next\",\"text\":\"next\"}\n"); err != nil {
		t.Fatal(err)
	}
	second := receiveInvocation(t, c.calls)
	if second.settings.Effort() != "low" {
		t.Fatal("next sample missed selection")
	}
	if err := send.Close(); err != nil {
		t.Fatal(err)
	}
	second.release <- completedProviderEvent(t)
	if result := <-done; !result.Completed {
		t.Fatal("EOF drain failed", result)
	}
	output.mu.Lock()
	text := output.buffer.String()
	output.mu.Unlock()
	if strings.Count(text, `"type":"effort.snapshot"`) != 1 || c.active.Load() != 0 {
		t.Fatal("unsolicited snapshots or leaked worker")
	}
}

func TestEffortAcceptedResponseThenSnapshotShortWriteCleansOwner(t *testing.T) {
	c := newControlledConversation()
	loop := effortLoop(t, c, &fakeJournal{})
	input := io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"effort.set\",\"request_id\":\"low\",\"mode\":\"value\",\"level\":\"low\"}\n"))
	output := newEffortStreamOutput()
	output.failSnapshot = true
	runner, err := headless.NewStreamRunner(loop, headless.StreamRunConfig{SessionID: runtimeSessionID, ThreadID: runtimeThreadID, Input: input, Output: output, OutputCloser: output})
	if err != nil {
		t.Fatal(err)
	}
	result := runner.Run(context.Background())
	if !result.OutputUnavailable {
		t.Fatal("broken snapshot succeeded")
	}
	response := effortFrame(t, output, `"command_type":"effort.set"`)
	attempt := effortFrame(t, output, `"type":"effort.snapshot"`)
	if !bytes.Contains(response, []byte(`"status":"accepted"`)) || !bytes.Contains(attempt, []byte(`"value":"low"`)) {
		t.Fatal("accepted setting not retained")
	}
	select {
	case <-loop.Done():
	default:
		t.Fatal("output failure abandoned owner")
	}
	if len(c.calls) != 0 {
		t.Fatal("output failure retried model")
	}
}

func TestEffortInteractiveFullBufferDetachAndCancelledAdmission(t *testing.T) {
	loop := effortLoop(t, newControlledConversation(), &fakeJournal{})
	host := NewChatSession(loop.runtime)
	if err := host.StartInteractive(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Dispatcher 先持有一项，再由九个双输出查询确定性填满十六项 owner buffer。
	for i := 0; i < 9; i++ {
		if result, err := host.QueryEffort(context.Background(), protocol.RequestID(fmt.Sprintf("fill-%d", i))); err != nil || result.Status() != protocol.CommandStatusAccepted {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := host.SetEffort(ctx, "unreceived", effortChoice(t, "low")); err == nil {
		t.Fatal("cancelled handoff changed selection")
	}
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-host.Done():
	default:
		t.Fatal("detach left dispatcher active")
	}
	for range host.Outputs() {
	}
	if len(loop.runtime.journal.(*fakeJournal).snapshot()) != 0 {
		t.Fatal("buffer controls persisted facts")
	}
}

func TestEffortCancellationAfterHandoffKeepsDefiniteResult(t *testing.T) {
	loop := effortLoop(t, newControlledConversation(), &fakeJournal{})
	loop.state.Store(agentLoopRunning)
	state := newAgentLoopRunState(loop, context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command, _ := protocol.NewSetEffortCommand("accepted", effortChoice(t, "low"))
	done := make(chan loopCommandResponse, 1)
	go func() {
		result, err := loop.Submit(ctx, command)
		done <- loopCommandResponse{result: result, err: err}
	}()
	request := <-loop.requests
	cancel()
	state.handleCommand(request)
	response := <-done
	if response.err != nil || response.result.Status() != protocol.CommandStatusAccepted || state.selectedProfile.InferenceSettings().Effort() != "low" {
		t.Fatal("post-handoff cancellation rolled back selection")
	}
}

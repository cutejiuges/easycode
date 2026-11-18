package headless

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

func TestRunTextDiscardsPartialOutputOnFailureAndProtocolError(t *testing.T) {
	failure := runtimeFailure(t, fault.Summary{Code: fault.CodeProviderRequest, Message: "provider request failed"})
	tests := []struct {
		name     string
		events   []protocol.Event
		wantCode fault.Code
	}{
		{name: "turn failure", events: []protocol.Event{runtimeStarted(), runtimeDelta(t, "partial"), failure}, wantCode: fault.CodeProviderRequest},
		{name: "closed early", events: []protocol.Event{runtimeStarted(), runtimeDelta(t, "partial")}, wantCode: fault.CodeStreamProtocol},
		{name: "identity drift", events: []protocol.Event{runtimeStarted(), withWrongThread(runtimeDelta(t, "partial"))}, wantCode: fault.CodeStreamProtocol},
		{name: "bad payload", events: []protocol.Event{runtimeStarted(), malformedDelta()}, wantCode: fault.CodeStreamProtocol},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			result := Run(context.Background(), fixedSession(test.events...), textConfig(&output))
			if result.Completed || result.Failure.Code != test.wantCode || output.Len() != 0 {
				t.Fatalf("result/output = %#v/%q", result, output.String())
			}
		})
	}
}

func TestRunCancellationUsesRuntimeTerminalAndInterruptsOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := cancellableSession(t)
	var output bytes.Buffer
	result := Run(ctx, session, jsonConfig(&output, false))
	if result.Failure.Code != fault.CodeUserCancelled || !result.Failure.Cancelled || !result.Reported {
		t.Fatalf("result = %#v", result)
	}
	if session.interrupts.Load() != 1 || strings.Count(output.String(), `"type":"turn.failed"`) != 1 {
		t.Fatalf("interrupts/output = %d/%s", session.interrupts.Load(), output.String())
	}
}

func TestRunCompletionWinsCancellationRace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	session := fixedSession(runtimeStarted(), runtimeCompleted())
	var output bytes.Buffer
	result := Run(ctx, session, textConfig(&output))
	if !result.Completed || session.interrupts.Load() != 0 || output.String() != "\n" {
		t.Fatalf("result/interrupts/output = %#v/%d/%q", result, session.interrupts.Load(), output.String())
	}
}

func TestRunWriterFailureInterruptsAndDrainsActiveTurn(t *testing.T) {
	session := cancellableSession(t)
	session.events <- runtimeDelta(t, "partial")
	writer := &failOnCallWriter{failCall: 3}
	result := Run(context.Background(), session, jsonConfig(writer, false))
	if result.Failure.Code != fault.CodeOutput || !result.OutputUnavailable || session.interrupts.Load() != 1 {
		t.Fatalf("result/interrupts = %#v/%d", result, session.interrupts.Load())
	}
}

type fakeSession struct {
	events     chan protocol.Event
	submitErr  error
	interrupt  func()
	interrupts atomic.Int32
	prompts    []string
	contexts   []context.Context
}

func (session *fakeSession) Submit(ctx context.Context, prompt string) (<-chan protocol.Event, error) {
	session.contexts = append(session.contexts, ctx)
	session.prompts = append(session.prompts, prompt)
	return session.events, session.submitErr
}

func (session *fakeSession) Interrupt() {
	session.interrupts.Add(1)
	if session.interrupt != nil {
		session.interrupt()
	}
}

func fixedSession(events ...protocol.Event) *fakeSession {
	stream := make(chan protocol.Event, len(events))
	for _, event := range events {
		stream <- event
	}
	close(stream)
	return &fakeSession{events: stream}
}

func cancellableSession(t *testing.T) *fakeSession {
	t.Helper()
	stream := make(chan protocol.Event, 4)
	stream <- runtimeStarted()
	session := &fakeSession{events: stream}
	var once sync.Once
	session.interrupt = func() {
		once.Do(func() {
			stream <- runtimeFailure(t, fault.Summary{
				Code: fault.CodeUserCancelled, Message: "turn was cancelled", Cancelled: true,
			})
			close(stream)
		})
	}
	return session
}

func textConfig(output ioWriter) RunConfig {
	return RunConfig{
		Mode: ModeText, Prompt: "hello", SessionID: testSessionID,
		ThreadID: testThreadID, Output: output,
	}
}

func jsonConfig(output ioWriter, resumed bool) RunConfig {
	config := textConfig(output)
	config.Mode = ModeJSON
	config.Resumed = resumed
	return config
}

type ioWriter interface {
	Write([]byte) (int, error)
}

func runtimeStarted() protocol.Event {
	event := protocol.NewTurnStarted()
	return decorateRuntime(event)
}

func runtimeDelta(t *testing.T, text string) protocol.Event {
	t.Helper()
	event, err := protocol.NewAssistantTextDelta(text)
	if err != nil {
		t.Fatal(err)
	}
	return decorateRuntime(event)
}

func runtimeCompleted() protocol.Event {
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(8),
		domain.KnownUsageMetric(2),
		domain.UnknownUsageMetric(),
		domain.KnownUsageMetric(5),
		domain.NotApplicableUsageMetric(),
	)
	if err != nil {
		panic(err)
	}
	event, err := protocol.NewTurnCompleted(usage)
	if err != nil {
		panic(err)
	}
	return decorateRuntime(event)
}

func runtimeFailure(t *testing.T, failure fault.Summary) protocol.Event {
	t.Helper()
	event, err := protocol.NewTurnFailed(string(failure.Code), failure.Message, failure.Cancelled)
	if err != nil {
		t.Fatal(err)
	}
	return decorateRuntime(event)
}

func decorateRuntime(event protocol.Event) protocol.Event {
	event.SessionID = testSessionID
	event.ThreadID = testThreadID
	event.TurnID = testTurnID
	return event
}

func withWrongThread(event protocol.Event) protocol.Event {
	event.ThreadID = "00000000-0099-7000-8000-000000000099"
	return event
}

func malformedDelta() protocol.Event {
	event := decorateRuntime(protocol.Event{
		Kind:    protocol.EventAssistantTextDelta,
		Payload: []byte(`{"text":"hello","unknown":true}`),
	})
	return event
}

type failOnCallWriter struct {
	calls    int
	failCall int
	buffer   bytes.Buffer
}

func (writer *failOnCallWriter) Write(content []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failCall {
		return 0, errors.New("fixture writer secret")
	}
	return writer.buffer.Write(content)
}

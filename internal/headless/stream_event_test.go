package headless

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"easycode/internal/fault"
	"easycode/internal/protocol"
)

func TestStreamProjectorAndEncoderMatchV1Fixture(t *testing.T) {
	projectorValue, err := NewStreamProjector(testSessionID, testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	events := []StreamEvent{mustStreamThreadStarted(t, true)}
	submit, _ := protocol.NewSubmitInputCommand("input-1", "hello")
	accepted, _ := protocol.NewAcceptedCommandResult(submit, protocol.CommandDispositionStarting)
	events = append(events, projectControlItem(t, projectorValue, mustResultItem(t, accepted)))
	events = append(events, projectControlItem(t, projectorValue, mustTurnItem(t, "input-1", decoratedRuntimeEvent(protocol.NewTurnStarted()))))
	delta, _ := protocol.NewAssistantTextDelta("hello")
	events = append(events, projectControlItem(t, projectorValue, mustTurnItem(t, "input-1", decoratedRuntimeEvent(delta))))
	completed, _ := protocol.NewTurnCompleted(testHeadlessUsage(t))
	events = append(events, projectControlItem(t, projectorValue, mustTurnItem(t, "input-1", decoratedRuntimeEvent(completed))))
	interrupt, _ := protocol.NewInterruptCommand("interrupt-1", testTurnID)
	rejected, _ := protocol.NewRejectedCommandResult(interrupt, protocol.ControlErrorTurnMismatch)
	events = append(events, projectControlItem(t, projectorValue, mustResultItem(t, rejected)))
	discard, _ := protocol.NewInputDiscardItem("input-2", protocol.ControlErrorSessionShutdown)
	events = append(events, projectControlItem(t, projectorValue, discard))
	streamError, err := NewStreamErrorEvent(fault.Summary{Code: fault.CodeStreamProtocol, Message: "stream input is invalid"})
	if err != nil {
		t.Fatal(err)
	}
	events = append(events, streamError)

	var output bytes.Buffer
	encoder := NewStreamEncoder(&output)
	for _, event := range events {
		if err := encoder.Write(event); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/stream-events-v1.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("stream JSONL differs\ngot:\n%s\nwant:\n%s", output.Bytes(), want)
	}
}

func TestStreamEncoderShortWritesAndPermanentFailure(t *testing.T) {
	t.Parallel()
	chunked := &chunkWriter{limit: 2}
	if err := NewStreamEncoder(chunked).Write(mustStreamThreadStarted(t, false)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(chunked.String(), "\n") {
		t.Fatalf("chunked output = %q", chunked.String())
	}
	broken := &brokenWriter{remaining: 7}
	encoder := NewStreamEncoder(broken)
	if err := encoder.Write(mustStreamThreadStarted(t, false)); err == nil {
		t.Fatal("broken output unexpectedly succeeded")
	}
	written := broken.buffer.Len()
	if err := encoder.Write(mustStreamThreadStarted(t, false)); err == nil || broken.buffer.Len() != written {
		t.Fatal("encoder wrote after permanent failure")
	}
}

func TestStreamingVocabularyDoesNotChangeOneShotJSON(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := NewEncoder(&output).Write(mustTurnStarted(t)); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"control.response", "input.discarded", "input_id"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("one-shot JSON contains %q: %s", forbidden, output.String())
		}
	}
}

func mustStreamThreadStarted(t *testing.T, resumed bool) StreamEvent {
	t.Helper()
	event, err := NewStreamThreadStartedEvent(testSessionID, testThreadID, resumed)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func decoratedRuntimeEvent(event protocol.Event) protocol.Event {
	event.SessionID = testSessionID
	event.ThreadID = testThreadID
	event.TurnID = testTurnID
	return event
}

func mustResultItem(t *testing.T, result protocol.CommandResult) protocol.ControlItem {
	t.Helper()
	item, err := protocol.NewCommandResultItem(result)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func mustTurnItem(t *testing.T, requestID protocol.RequestID, event protocol.Event) protocol.ControlItem {
	t.Helper()
	item, err := protocol.NewCorrelatedTurnItem(requestID, event)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func projectControlItem(t *testing.T, projectorValue *StreamProjector, item protocol.ControlItem) StreamEvent {
	t.Helper()
	event, err := projectorValue.Project(item)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

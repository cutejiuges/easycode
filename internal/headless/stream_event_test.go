package headless

import (
	"bytes"
	"os"
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

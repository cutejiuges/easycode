package protocol

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/domain"
)

const (
	testRequestID = RequestID("request-1")
	testTurnID    = domain.TurnID("00000000-0012-7000-8000-000000000012")
	testSessionID = domain.SessionID("00000000-0010-7000-8000-000000000010")
	testThreadID  = domain.ThreadID("00000000-0011-7000-8000-000000000011")
)

func TestCommandConstructorsAndStrictDecoder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		wire string
		kind CommandKind
	}{
		{name: "submit", wire: `{"version":1,"kind":"submit_input","request_id":"request-1","text":"你好"}`, kind: CommandSubmitInput},
		{name: "interrupt", wire: `{"version":1,"kind":"interrupt","request_id":"request-1","expected_turn_id":"00000000-0012-7000-8000-000000000012"}`, kind: CommandInterrupt},
		{name: "shutdown", wire: `{"version":1,"kind":"shutdown","request_id":"request-1"}`, kind: CommandShutdown},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command, err := DecodeCommand([]byte(test.wire))
			if err != nil {
				t.Fatal(err)
			}
			if command.Version() != CurrentVersion || command.Kind() != test.kind || command.RequestID() != testRequestID {
				t.Fatalf("command = %#v", command)
			}
			if err := command.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}

	submit, err := NewSubmitInputCommand(testRequestID, "hello")
	if err != nil || submit.Text() != "hello" {
		t.Fatalf("submit = %#v, %v", submit, err)
	}
	interrupt, err := NewInterruptCommand(testRequestID, testTurnID)
	if err != nil || interrupt.ExpectedTurnID() != testTurnID {
		t.Fatalf("interrupt = %#v, %v", interrupt, err)
	}
	if _, err := NewShutdownCommand(testRequestID); err != nil {
		t.Fatal(err)
	}
}

func TestCommandRejectsInvalidWireWithoutEchoingInput(t *testing.T) {
	t.Parallel()
	invalidUTF8 := append([]byte(`{"version":1,"kind":"submit_input","request_id":"request-1","text":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	tests := map[string][]byte{
		"missing field":    []byte(`{"version":1,"kind":"submit_input","request_id":"request-1"}`),
		"unknown field":    []byte(`{"version":1,"kind":"shutdown","request_id":"request-1","secret":"do-not-echo"}`),
		"duplicate field":  []byte(`{"version":1,"kind":"shutdown","request_id":"request-1","request_id":"request-2"}`),
		"trailing value":   []byte(`{"version":1,"kind":"shutdown","request_id":"request-1"} {}`),
		"unknown revision": []byte(`{"version":2,"kind":"shutdown","request_id":"request-1"}`),
		"unknown kind":     []byte(`{"version":1,"kind":"future","request_id":"request-1"}`),
		"invalid ID":       []byte(`{"version":1,"kind":"shutdown","request_id":" bad "}`),
		"invalid turn":     []byte(`{"version":1,"kind":"interrupt","request_id":"request-1","expected_turn_id":"bad"}`),
		"extra payload":    []byte(`{"version":1,"kind":"shutdown","request_id":"request-1","text":"secret"}`),
		"multiple values":  []byte(`{} {}`),
		"invalid UTF-8":    invalidUTF8,
	}
	for name, wire := range tests {
		wire := wire
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeCommand(wire); err == nil {
				t.Fatal("DecodeCommand() unexpectedly succeeded")
			} else if strings.Contains(err.Error(), "do-not-echo") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked input: %v", err)
			}
		})
	}
}

func TestSubmitInputValidationBoundaries(t *testing.T) {
	t.Parallel()
	valid := strings.Repeat("x", MaxInputBytes)
	if _, err := NewSubmitInputCommand(testRequestID, valid); err != nil {
		t.Fatalf("4 MiB input rejected: %v", err)
	}
	for name, input := range map[string]string{
		"blank":       " \n\t",
		"oversized":   valid + "x",
		"invalidUTF8": string([]byte{0xff}),
	} {
		if _, err := NewSubmitInputCommand(testRequestID, input); err == nil {
			t.Errorf("%s input unexpectedly accepted", name)
		}
	}
	if !utf8.ValidString(valid) {
		t.Fatal("boundary fixture is invalid")
	}
	if _, err := ParseRequestID(strings.Repeat("x", MaxRequestIDBytes)); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", " id", "id\n", strings.Repeat("x", MaxRequestIDBytes+1), string([]byte{0xff})} {
		if _, err := ParseRequestID(value); err == nil {
			t.Errorf("ParseRequestID(%q) unexpectedly succeeded", value)
		}
	}
}

func TestCommandResultsAndControlItemsAreClosedAndSafe(t *testing.T) {
	t.Parallel()
	submit, _ := NewSubmitInputCommand(testRequestID, "hello")
	accepted, err := NewAcceptedCommandResult(submit, CommandDispositionStarting)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status() != CommandStatusAccepted || accepted.Disposition() != CommandDispositionStarting {
		t.Fatalf("accepted = %#v", accepted)
	}
	if _, err := NewAcceptedCommandResult(submit, CommandDispositionInterrupting); err == nil {
		t.Fatal("kind/disposition mismatch was accepted")
	}
	for _, code := range []ControlErrorCode{
		ControlErrorInvalidCommand, ControlErrorInvalidInput, ControlErrorInputQueueFull,
		ControlErrorDuplicateRequest, ControlErrorNoActiveTurn, ControlErrorTurnMismatch,
		ControlErrorSessionClosing, ControlErrorSessionShutdown, ControlErrorSessionFailed,
	} {
		result, err := NewRejectedCommandResult(submit, code)
		if err != nil {
			t.Fatal(err)
		}
		summary, ok := result.Error()
		if !ok || summary.Code() != code || strings.Contains(summary.Message(), "secret") {
			t.Fatalf("summary = %#v, %v", summary, ok)
		}
	}

	resultItem, err := NewCommandResultItem(accepted)
	if err != nil || resultItem.Kind() != ControlItemCommandResult {
		t.Fatalf("result item = %#v, %v", resultItem, err)
	}
	event := NewTurnStarted()
	event.SessionID, event.ThreadID, event.TurnID = testSessionID, testThreadID, testTurnID
	turnItem, err := NewCorrelatedTurnItem(testRequestID, event)
	if err != nil || turnItem.Kind() != ControlItemTurnEvent {
		t.Fatalf("turn item = %#v, %v", turnItem, err)
	}
	discard, err := NewInputDiscardItem(testRequestID, ControlErrorSessionShutdown)
	if err != nil || discard.Kind() != ControlItemInputDiscard {
		t.Fatalf("discard = %#v, %v", discard, err)
	}
	if _, err := NewInputDiscardItem(testRequestID, ControlErrorInvalidInput); err == nil {
		t.Fatal("invalid discard reason was accepted")
	}
	if err := (Command{}).Validate(); err == nil {
		t.Fatal("zero command was accepted")
	}
	if err := (CommandResult{}).Validate(); err == nil {
		t.Fatal("zero result was accepted")
	}
	if err := (ControlItem{}).Validate(); err == nil {
		t.Fatal("zero control item was accepted")
	}

	// 构造器不接收 cause，固定摘要不能携带带密钥的底层错误文本。
	if bytes.Contains([]byte(discard.discard.Message()), []byte("Authorization")) {
		t.Fatal("discard summary leaked a cause")
	}
}

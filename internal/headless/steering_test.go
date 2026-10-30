package headless

import (
	"easycode/internal/protocol"
	"strings"
	"testing"
)

func TestSteeringCommandsStrictWire(t *testing.T) {
	for _, tc := range []struct {
		body string
		kind protocol.CommandKind
	}{
		{`{"version":1,"type":"input.append","request_id":"a","text":"你好\n继续"}`, protocol.CommandSubmitConversationalInput},
		{`{"version":1,"type":"input.retract","request_id":"r","input_ids":["00000000-0013-7000-8000-000000000013"]}`, protocol.CommandRetractInputs},
		{`{"version":1,"type":"turn.stop","request_id":"s","expected_turn_id":"00000000-0012-7000-8000-000000000012"}`, protocol.CommandStopTurn},
	} {
		reader, err := NewStreamCommandReader(strings.NewReader(tc.body + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		command, err := reader.ReadCommand()
		if err != nil || command.Kind() != tc.kind {
			t.Fatalf("command: %v", err)
		}
		for _, bad := range []string{strings.TrimSuffix(tc.body, "}") + `,"unknown":1}`, strings.TrimSuffix(tc.body, "}") + `,"request_id":"duplicate"}`, strings.Replace(tc.body, `"request_id":"`, `"request_id":null,"ignored":"`, 1)} {
			if _, err := DecodeStreamCommand([]byte(bad)); err == nil {
				t.Fatalf("accepted %s", bad)
			}
		}
	}
	for _, ids := range []string{`null`, `[]`, `[null]`, `["bad"]`, `["00000000-0013-7000-8000-000000000013","00000000-0013-7000-8000-000000000013"]`} {
		if _, err := DecodeStreamCommand([]byte(`{"version":1,"type":"input.retract","request_id":"r","input_ids":` + ids + `}`)); err == nil {
			t.Fatalf("accepted IDs %s", ids)
		}
	}
}

func TestInputStatusWireGoldenOmitsBody(t *testing.T) {
	var output strings.Builder
	encoder := NewStreamEncoder(&output)
	event := StreamInputStatusEvent{Version: 1, Type: "input.status", RequestID: "request-1", EntryID: "01900000-0000-7000-8000-000000000001", State: protocol.InputReturned, Reason: protocol.InputReasonRetracted}
	if err := encoder.Write(event); err != nil {
		t.Fatal(err)
	}
	const want = `{"version":1,"type":"input.status","request_id":"request-1","entry_id":"01900000-0000-7000-8000-000000000001","state":"returned","reason":"retracted"}` + "\n"
	if output.String() != want {
		t.Fatalf("wire shape changed: %s", output.String())
	}
	event.State = protocol.InputUncertain
	if err := encoder.Write(event); err == nil {
		t.Fatal("uncertain without turn accepted")
	}
}

package protocol

import (
	"testing"

	"easycode/internal/domain"
)

func TestWorkModeControlRejectsInvalidPayloadAndScope(t *testing.T) {
	if _, err := NewSetWorkModeCommand(testRequestID, domain.WorkMode("unknown")); err == nil {
		t.Fatal("unknown work mode was accepted")
	}
	event, err := NewWorkModeSnapshot(testSessionID, testThreadID, domain.WorkModePlan)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `{"mode":"plan","extra":1}`, `{"mode":"plan","mode":"default"}`} {
		bad := event
		bad.Payload = []byte(raw)
		if _, err := NewCorrelatedContextItem(testRequestID, bad); err == nil {
			t.Fatalf("bad mode snapshot accepted: %s", raw)
		}
	}
	event.TurnID = testTurnID
	if _, err := NewCorrelatedContextItem(testRequestID, event); err == nil {
		t.Fatal("mode confirmation accepted a turn identity")
	}
}

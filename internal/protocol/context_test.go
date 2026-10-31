package protocol

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestContextCommandsRemainSeparateFromTurnInput(t *testing.T) {
	query, err := NewQueryContextCommand("query")
	if err != nil || query.Text() != "" || query.ExpectedTurnID() != "" {
		t.Fatal("query acquired turn payload")
	}
	compact, err := NewCompactContextCommand("compact", " \nkeep tests\nand files \n")
	if err != nil || compact.Instructions() != "keep tests\nand files" || compact.Text() != "" {
		t.Fatal("instructions changed")
	}
	for _, bad := range []string{strings.Repeat("x", (16<<10)+1), string([]byte{0xff})} {
		if _, err := NewCompactContextCommand("c", bad); err == nil {
			t.Fatal("invalid instructions accepted")
		}
	}
	if _, err := NewCancelCompactionCommand("cancel", ""); err == nil {
		t.Fatal("untargeted cancellation accepted")
	}
	target := domain.CompactionID("01900000-0000-7000-8000-000000000001")
	cancel, err := NewCancelCompactionCommand("cancel", target)
	if err != nil || cancel.ExpectedCompactionID() != target {
		t.Fatal("cancel lost identity")
	}
	query.expectedCompactionID = target
	if query.Validate() == nil {
		t.Fatal("query accepted compaction identity")
	}
	compact.expectedTurnID = domain.TurnID(target)
	if compact.Validate() == nil {
		t.Fatal("compaction accepted turn identity")
	}
}

func TestCompactionEventScopesAndStrictDecoders(t *testing.T) {
	sessionID := domain.SessionID("01900000-0000-7000-8000-000000000001")
	threadID := domain.ThreadID("01900000-0000-7000-8000-000000000002")
	id := domain.CompactionID("01900000-0000-7000-8000-000000000003")
	started, err := NewCompactionStarted(sessionID, threadID, id, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCorrelatedTurnItem("compact", started); err == nil {
		t.Fatal("compaction accepted as turn event")
	}
	correlated, err := NewCorrelatedContextItem("compact", started)
	if err != nil {
		t.Fatal(err)
	}
	copy, _ := correlated.ContextEvent()
	copy.Payload[0] = '['
	again, _ := correlated.ContextEvent()
	if again.Validate() != nil {
		t.Fatal("event getter leaked payload storage")
	}
	for _, modify := range []func(*Event){func(e *Event) { e.TurnID = domain.TurnID(id) }, func(e *Event) { e.CompactionID = "" }, func(e *Event) { e.SessionID = "" }, func(e *Event) { e.Kind = EventTurnStarted }} {
		value := started
		modify(&value)
		if value.Validate() == nil {
			t.Fatal("wrong event scope accepted")
		}
	}
	for _, payload := range []string{`{}`, `{"context_revision":null}`, `{"context_revision":2,"unknown":true}`, `{"context_revision":2,"context_revision":3}`} {
		value := started
		value.Payload = []byte(payload)
		if value.Validate() == nil {
			t.Fatal("invalid payload accepted")
		}
	}
	failed, err := NewCompactionFailed(sessionID, threadID, id, CompactionFailedPayload{Code: CompactionFailureCancelled, Durable: false, UsageState: "unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	failed.Payload = []byte(`{"code":"compaction_cancelled","usage_state":"unavailable"}`)
	if failed.Validate() == nil {
		t.Fatal("missing durable flag accepted")
	}
	command, _ := NewCompactContextCommand("compact", "")
	if _, err := NewAcceptedCommandResult(command, CommandDispositionCompacting); err == nil {
		t.Fatal("accepted compaction without identity")
	}
	result, err := NewAcceptedCompactionResult(command, id)
	if err != nil || result.CompactionID() != id {
		t.Fatal("invalid compaction admission")
	}
}

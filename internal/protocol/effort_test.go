package protocol

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func effortFixture() EffortSnapshotPayload {
	omitted := EffortValuePayload{State: "omitted"}
	return EffortSnapshotPayload{Scope: "process", Family: domain.ProviderOpenAI, Startup: omitted, Selected: omitted, Source: "startup", Activity: "idle", AllowedLevels: []domain.EffortLevel{"none", "minimal", "low", "medium", "high", "xhigh", "max"}}
}

func TestEffortCommandClosedPayloadAndResults(t *testing.T) {
	choice, _ := domain.NewExplicitEffortChoice("high")
	set, err := NewSetEffortCommand("set", choice)
	if err != nil || set.EffortChoice() != choice || set.Text() != "" {
		t.Fatal("invalid set", err)
	}
	query, err := NewQueryEffortCommand("query")
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []Command{set, query} {
		unrelated := command
		unrelated.text = "secret"
		if unrelated.Validate() == nil {
			t.Fatal("effort accepted input")
		}
		unrelated = command
		unrelated.expectedTurnID = "01900000-0000-7000-8000-000000000001"
		if unrelated.Validate() == nil {
			t.Fatal("effort accepted turn")
		}
		unrelated = command
		unrelated.instructions = "secret"
		if unrelated.Validate() == nil {
			t.Fatal("effort accepted compaction")
		}
	}
	query.effortChoice = choice
	if query.Validate() == nil {
		t.Fatal("query accepted choice")
	}
	submit, _ := NewSubmitInputCommand("input", "literal /effort")
	submit.effortChoice = choice
	if submit.Validate() == nil {
		t.Fatal("input accepted choice")
	}
	if _, err := NewSetEffortCommand("zero", domain.EffortChoice{}); err == nil {
		t.Fatal("zero choice accepted")
	}
	result, err := NewAcceptedCommandResult(set, CommandDispositionUpdated)
	if err != nil || result.Validate() != nil {
		t.Fatal(err)
	}
	if _, err := NewAcceptedCommandResult(set, CommandDispositionQueried); err == nil {
		t.Fatal("wrong disposition")
	}
	for _, code := range []ControlErrorCode{ControlErrorInvalidEffort, ControlErrorEffortConflict} {
		result, err := NewRejectedCommandResult(set, code)
		if err != nil {
			t.Fatal(err)
		}
		safe, _ := result.Error()
		if safe.Code() != code || strings.Contains(safe.Message(), "secret") {
			t.Fatal("unsafe rejection")
		}
	}
}

func TestEffortSnapshotsGoldenAndStrictUnion(t *testing.T) {
	sessionID := domain.SessionID("01900000-0000-7000-8000-000000000001")
	threadID := domain.ThreadID("01900000-0000-7000-8000-000000000002")
	high := EffortValuePayload{State: "explicit", Value: "high"}
	starting, _ := NewStartingEffortActive("original-input", "starting", high, 0, false)
	turn, _ := NewTurnEffortActive("01900000-0000-7000-8000-000000000003", "running", high, 0, false)
	compact, _ := NewCompactionEffortActive("01900000-0000-7000-8000-000000000004", "finishing", high, 0, true)
	var golden bytes.Buffer
	for _, active := range []*EffortActivePayload{nil, &starting, &turn, &compact} {
		payload := effortFixture()
		if active != nil {
			payload.Startup = high
			payload.Source = "auto"
			payload.Active = active
			payload.SelectionRevision = 1
			payload.Activity = "turn"
			payload.Pending = true
			if active.Kind() == "manual_compaction" {
				payload.Activity = "compacting"
			}
		}
		event, err := NewEffortSnapshot(sessionID, threadID, payload)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeEffortSnapshot(event)
		if err != nil || decoded.SelectionRevision != payload.SelectionRevision || decoded.Pending != payload.Pending {
			t.Fatal("lost snapshot", err)
		}
		item, err := NewCorrelatedContextItem("q", event)
		if err != nil || item.Validate() != nil {
			t.Fatal(err)
		}
		if _, err := NewCorrelatedTurnItem("q", event); err == nil {
			t.Fatal("query became turn event")
		}
		golden.Write(event.Payload)
		golden.WriteByte('\n')
	}
	path := "testdata/effort_snapshots.golden.jsonl"
	if os.Getenv("EASYCODE_UPDATE_EFFORT_GOLDENS") == "1" {
		if err := os.WriteFile(path, golden.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(want, golden.Bytes()) {
		t.Fatal("effort snapshot golden changed", err)
	}
	raw, _ := codec.MarshalStable(turn)
	for _, bad := range []string{
		strings.Replace(string(raw), `"selection_revision":0,`, "", 1),
		strings.Replace(string(raw), `"selection_revision":0`, `"selection_revision":18446744073709551616`, 1),
		strings.Replace(string(raw), `"selection_revision":0`, `"selection_revision":null`, 1),
		strings.Replace(string(raw), `"turn_id":`, `"request_id":"","turn_id":`, 1),
		strings.Replace(string(raw), `"turn_id":`, `"compaction_id":null,"turn_id":`, 1),
		strings.Replace(string(raw), `"phase":"running"`, `"phase":"starting"`, 1),
		strings.Replace(string(raw), `"kind":"turn"`, `"kind":"turn_starting"`, 1),
		strings.Replace(string(raw), `"cancel_requested":false`, `"cancel_requested":false,"cancel_requested":true`, 1),
	} {
		var active EffortActivePayload
		if codec.UnmarshalRecord([]byte(bad), &active) == nil {
			t.Fatal("invalid active accepted", bad)
		}
	}
	payload := effortFixture()
	payload.Active = &turn
	payload.Activity = "turn"
	payload.Pending = true
	future := turn
	future.wire.SelectionRevision = 1
	payload.Active = &future
	if payload.Validate() == nil {
		t.Fatal("future capture accepted")
	}
	base, _ := NewEffortSnapshot(sessionID, threadID, effortFixture())
	for _, bad := range []string{
		strings.Replace(string(base.Payload), `"selection_revision":0,`, "", 1),
		strings.Replace(string(base.Payload), `"activity":"idle"`, `"active":null,"activity":"idle"`, 1),
		strings.Replace(string(base.Payload), `"state":"omitted"`, `"state":"omitted","value":""`, 1),
		strings.Replace(string(base.Payload), `"source":"startup"`, `"source":"startup","unknown":true`, 1),
		strings.Repeat(" ", MaxEffortSnapshotBytes) + string(base.Payload),
	} {
		base.Payload = []byte(bad)
		if _, err := DecodeEffortSnapshot(base); err == nil {
			t.Fatal("invalid snapshot accepted", bad)
		}
	}
}

func TestContextInternalShapeRequiresEffortAndExternalMetricsRemainSeparate(t *testing.T) {
	raw := []byte(`{"cache_policy":{"anthropic":"disabled","openai_key":"omit","openai_transport":"http_sse"}}`)
	event := newEvent(EventContextSnapshot)
	event.Payload = raw
	if _, err := DecodeContextSnapshot(event); err == nil {
		t.Fatal("incomplete internal context accepted")
	}
	var value EffortValuePayload
	for _, raw := range []string{`{"state":"explicit"}`, `{"state":"omitted","value":null}`, `{"state":"explicit","value":""}`, `{"state":"omitted","extra":0}`} {
		if codec.UnmarshalRecord([]byte(raw), &value) == nil {
			t.Fatal("bad value accepted", raw)
		}
	}
}

func TestEffortStartingCannotClaimCompactionActivity(t *testing.T) {
	payload := effortFixture()
	active, err := NewStartingEffortActive("not-durable", "starting", payload.Selected, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	payload.Active, payload.Activity = &active, "turn"
	event, err := NewEffortSnapshot("01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002", payload)
	if err != nil {
		t.Fatal(err)
	}
	payload.Activity = "compacting"
	if payload.Validate() == nil {
		t.Fatal("non-durable turn claimed an automatic summary")
	}
	event.Payload = bytes.Replace(event.Payload, []byte(`"activity":"turn"`), []byte(`"activity":"compacting"`), 1)
	if _, err := DecodeEffortSnapshot(event); err == nil {
		t.Fatal("decoder accepted impossible starting activity")
	}
}

func TestEffortRevisionAndSelectionStayConsistent(t *testing.T) {
	high := EffortValuePayload{State: "explicit", Value: "high"}
	low := EffortValuePayload{State: "explicit", Value: "low"}
	active, _ := NewTurnEffortActive("01900000-0000-7000-8000-000000000003", "running", high, 1, false)
	payload := effortFixture()
	payload.Startup, payload.Selected, payload.Source = high, low, "runtime"
	payload.SelectionRevision, payload.Activity, payload.Active, payload.Pending = 2, "turn", &active, true
	event, err := NewEffortSnapshot("01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002", payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{
		bytes.Replace(event.Payload, []byte(`"selection_revision":2`), []byte(`"selection_revision":1`), 1),
		bytes.Replace(event.Payload, []byte(`"selection_revision":2`), []byte(`"selection_revision":0`), 1),
	} {
		event.Payload = raw
		if _, err := DecodeEffortSnapshot(event); err == nil {
			t.Fatal("inconsistent revision accepted", string(raw))
		}
	}
	payload = effortFixture()
	payload.Source = "auto"
	if payload.Validate() == nil {
		t.Fatal("initial revision claimed a non-startup choice")
	}
}

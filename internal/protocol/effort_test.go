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
		strings.Replace(string(raw), `"value":"high"`, `"value":"turbo"`, 1),
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
	anthropic := effortFixture()
	anthropic.Family = domain.ProviderAnthropic
	anthropic.AllowedLevels = []domain.EffortLevel{"low", "medium", "high", "xhigh", "max"}
	anthropic.Selected = EffortValuePayload{State: "explicit", Value: "minimal"}
	anthropic.Source = "runtime"
	anthropic.SelectionRevision = 1
	if anthropic.Validate() == nil {
		t.Fatal("family did not narrow the shared effort shape")
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

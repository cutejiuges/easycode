package headless

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestEffortCommandsStrictBranchesAndBoundedLine(t *testing.T) {
	for _, line := range []string{
		`{"version":1,"type":"effort.query","request_id":"查询🙂"}`,
		`{"version":1,"type":"effort.set","request_id":"a","mode":"auto"}`,
		`{"version":1,"type":"effort.set","request_id":"r","mode":"reset"}`,
		`{"version":1,"type":"effort.set","request_id":"v","mode":"value","level":"none"}`,
	} {
		reader, err := NewStreamCommandReader(iotest.OneByteReader(strings.NewReader(line)))
		if err != nil {
			t.Fatal(err)
		}
		command, err := reader.ReadCommand()
		if err != nil || command.Validate() != nil {
			t.Fatal("valid effort frame rejected", err)
		}
		if _, err := reader.ReadCommand(); err != io.EOF {
			t.Fatal("EOF not drained")
		}
	}
	prefix := `{"version":1,"type":"effort.set","request_id":"x",`
	for _, tail := range []string{
		`"mode":"AUTO"}`, `"mode":"unset"}`, `"mode":"value","level":"HIGH"}`, `"mode":"value"}`, `"mode":"value","level":null}`, `"mode":"value","level":"future"}`,
		`"mode":"auto","level":""}`, `"mode":"reset","level":"high"}`, `"mode":null}`, `"mode":"auto","extra":"secret"}`, `"mode":"auto","mode":"reset"}`,
	} {
		_, err := DecodeStreamCommand([]byte(prefix + tail))
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid effort frame or unsafe error", tail, err)
		}
	}
	query := `{"version":1,"type":"effort.query","request_id":"q"}`
	if _, err := DecodeStreamCommand([]byte(query + strings.Repeat(" ", MaxEffortCommandBytes-len(query)))); err != nil {
		t.Fatal("limit rejected", err)
	}
	if _, err := DecodeStreamCommand([]byte(query + strings.Repeat(" ", MaxEffortCommandBytes-len(query)+1))); err == nil {
		t.Fatal("effort limit ignored")
	}
	for _, line := range []string{strings.Replace(query, `"q"`, `null`, 1), strings.Replace(query, `"q"`, `"q","mode":"auto"`, 1), strings.Replace(query, `"version":1,`, "", 1)} {
		if _, err := DecodeStreamCommand([]byte(line)); err == nil {
			t.Fatal("query shape accepted", line)
		}
	}
	literal, err := DecodeStreamCommand([]byte(`{"version":1,"type":"input.submit","request_id":"literal","text":"/effort high"}`))
	if err != nil || literal.Kind() != protocol.CommandSubmitInput || literal.Text() != "/effort high" {
		t.Fatal("slash input interpreted")
	}
}

func TestEffortWireGoldenAndContextProjectionUnchanged(t *testing.T) {
	sessionID := domain.SessionID("01900000-0000-7000-8000-000000000001")
	threadID := domain.ThreadID("01900000-0000-7000-8000-000000000002")
	projector, err := NewStreamProjector(sessionID, threadID)
	if err != nil {
		t.Fatal(err)
	}
	choice, _ := domain.NewExplicitEffortChoice("low")
	command, _ := protocol.NewSetEffortCommand("set", choice)
	accepted, _ := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionUpdated)
	response, _ := protocol.NewCommandResultItem(accepted)
	high := protocol.EffortValuePayload{State: "explicit", Value: "high"}
	low := protocol.EffortValuePayload{State: "explicit", Value: "low"}
	active, _ := protocol.NewStartingEffortActive("original", "starting", high, 0, false)
	snapshot := protocol.EffortSnapshotPayload{Scope: "process", Family: domain.ProviderOpenAI, Startup: high, Selected: low, Source: "runtime", SelectionRevision: 1, Activity: "turn", Active: &active, Pending: true, AllowedLevels: []domain.EffortLevel{"none", "minimal", "low", "medium", "high", "xhigh", "max"}}
	event, err := protocol.NewEffortSnapshot(sessionID, threadID, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	item, _ := protocol.NewCorrelatedContextItem("set", event)
	var output bytes.Buffer
	for _, item := range []protocol.ControlItem{response, item} {
		wire, err := projector.Project(item)
		if err != nil {
			t.Fatal(err)
		}
		if err := NewStreamEncoder(&output).Write(wire); err != nil {
			t.Fatal(err)
		}
	}
	path := "testdata/effort.golden.jsonl"
	if os.Getenv("EASYCODE_UPDATE_EFFORT_GOLDENS") == "1" {
		if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(want, output.Bytes()) {
		t.Fatal("effort wire golden changed", err)
	}
	if !strings.HasPrefix(output.String(), `{"version":1,"type":"control.response","request_id":"set","command_type":"effort.set"`) || strings.Count(output.String(), "effort.snapshot") != 1 || strings.Contains(output.String(), "schema_version") {
		t.Fatal("wire correlation or shape changed", output.String())
	}
	wrong := StreamControlResponseEvent{Version: 1, Type: "control.response", RequestID: "x", CommandType: "effort.set", Status: protocol.CommandStatusAccepted, Disposition: protocol.CommandDispositionQueried}
	if validateStreamEvent(wrong) == nil {
		t.Fatal("set accepted queried disposition")
	}
}

func TestContextExternalMetricsGoldenExcludesInternalEffort(t *testing.T) {
	policy, _ := protocol.NewCachePolicyPayload(domain.DefaultCachePolicy())
	tokens := uint64(4)
	estimate := protocol.ContextEstimatePayload{Method: "byte_heuristic_v1", State: domain.TokenEstimateEstimated, Tokens: &tokens}
	metrics := protocol.ContextMetricsPayload{CachePolicy: policy, AutoCompactionState: "disabled", AutoCompactionReason: "disabled", Scope: "committed", Revision: 1, Activity: "idle", Model: "fixture-model", Sources: []protocol.ContextSourcePayload{{Kind: "provider_profile", Revision: "profile", Estimate: estimate}, {Kind: "tool_catalog", Revision: "tools", Estimate: estimate}, {Kind: "committed_history", Revision: "native", Estimate: estimate}, {Kind: "current_input", Revision: "input", Estimate: estimate}}, Total: estimate, BudgetState: "not_enforced"}
	omitted := protocol.EffortValuePayload{State: "omitted"}
	effort := protocol.EffortSnapshotPayload{Scope: "process", Family: domain.ProviderOpenAI, Startup: omitted, Selected: omitted, Source: "startup", Activity: "idle", AllowedLevels: []domain.EffortLevel{"none", "minimal", "low", "medium", "high", "xhigh", "max"}}
	sid := domain.SessionID("01900000-0000-7000-8000-000000000001")
	tid := domain.ThreadID("01900000-0000-7000-8000-000000000002")
	event, err := protocol.NewContextSnapshot(sid, tid, protocol.ContextSnapshotPayload{ContextMetricsPayload: metrics, Effort: effort})
	if err != nil {
		t.Fatal(err)
	}
	old, err := codec.MarshalStable(metrics)
	if err != nil {
		t.Fatal(err)
	}
	missing := event
	missing.Payload = old
	if _, err := protocol.DecodeContextSnapshot(missing); err == nil {
		t.Fatal("old internal metrics-only shape accepted")
	}
	item, _ := protocol.NewCorrelatedContextItem("context", event)
	projector, _ := NewStreamProjector(sid, tid)
	wire, err := projector.Project(item)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := NewStreamEncoder(&output).Write(wire); err != nil {
		t.Fatal(err)
	}
	path := "testdata/context_metrics.golden.jsonl"
	if os.Getenv("EASYCODE_UPDATE_EFFORT_GOLDENS") == "1" {
		if err := os.WriteFile(path, output.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(want, output.Bytes()) {
		t.Fatal("external context metrics changed", err)
	}
	if bytes.Contains(output.Bytes(), []byte("effort")) || bytes.Contains(output.Bytes(), []byte("selection_revision")) {
		t.Fatal("internal setting leaked into old external context shape")
	}
}

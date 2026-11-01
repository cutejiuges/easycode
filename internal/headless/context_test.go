package headless

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestStrictContextStreamCommands(t *testing.T) {
	for _, line := range []string{
		`{"version":1,"type":"context.query","request_id":"q"}`,
		`{"version":1,"type":"context.compact","request_id":"c","instructions":""}`,
		`{"version":1,"type":"context.cancel","request_id":"x","expected_compaction_id":"01900000-0000-7000-8000-000000000001"}`,
	} {
		if _, err := DecodeStreamCommand([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	for _, line := range []string{
		`{"version":1,"type":"context.query","request_id":"q","text":"leak"}`,
		`{"version":1,"type":"context.compact","request_id":"c"}`,
		`{"version":1,"type":"context.compact","request_id":"c","instructions":null}`,
		`{"version":1,"type":"context.compact","request_id":"c","instructions":"a","instructions":"b"}`,
		`{"version":1,"type":"context.cancel","request_id":"x","expected_turn_id":"01900000-0000-7000-8000-000000000001"}`,
		`{"version":1,"type":"context.cancel","request_id":"x","expected_compaction_id":null}`,
		`{"version":1,"type":"context.compact","request_id":"c","instructions":"` + strings.Repeat("x", (16<<10)+1) + `"}`,
	} {
		if _, err := DecodeStreamCommand([]byte(line)); err == nil {
			t.Fatal("invalid context command accepted")
		}
	}
}

func TestCompactionStreamProjectionHasIndependentIdentity(t *testing.T) {
	sessionID := domain.SessionID("01900000-0000-7000-8000-000000000001")
	threadID := domain.ThreadID("01900000-0000-7000-8000-000000000002")
	id := domain.CompactionID("01900000-0000-7000-8000-000000000003")
	projector, err := NewStreamProjector(sessionID, threadID)
	if err != nil {
		t.Fatal(err)
	}
	command, _ := protocol.NewCompactContextCommand("c", "")
	accepted, _ := protocol.NewAcceptedCompactionResult(command, id)
	result, _ := protocol.NewCommandResultItem(accepted)
	started, _ := protocol.NewCompactionStarted(sessionID, threadID, id, 2, domain.ManualCompactionScope())
	startItem, _ := protocol.NewCorrelatedContextItem("c", started)
	failed, _ := protocol.NewCompactionFailed(sessionID, threadID, id, protocol.CompactionFailedPayload{Scope: domain.ManualCompactionScope(), Code: protocol.CompactionFailureCancelled, Durable: true, UsageState: "unavailable"})
	failItem, _ := protocol.NewCorrelatedContextItem("c", failed)
	var output bytes.Buffer
	encoder := NewStreamEncoder(&output)
	for _, item := range []protocol.ControlItem{result, startItem, failItem} {
		event, err := projector.Project(item)
		if err != nil {
			t.Fatal(err)
		}
		if err := encoder.Write(event); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/compaction.golden.jsonl")
	if err != nil || !bytes.Equal(want, output.Bytes()) {
		t.Fatal("compaction wire golden changed")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"disposition":"compacting"`) || !strings.Contains(lines[1], `"type":"compaction.started"`) || !strings.Contains(lines[2], `"durable":true`) {
		t.Fatalf("incorrect projection: %s", output.String())
	}
	for _, line := range lines {
		if strings.Contains(line, `"turn_id"`) || strings.Contains(line, `"input_id"`) || strings.Contains(line, `"payload"`) {
			t.Fatal("compaction leaked unrelated fields")
		}
	}
}

func TestContextCommandReaderUTF8Fragments(t *testing.T) {
	reader, err := NewStreamCommandReader(iotest.OneByteReader(strings.NewReader(`{"version":1,"type":"context.compact","request_id":"compact","instructions":"保留失败测试"}` + "\n")))
	if err != nil {
		t.Fatal(err)
	}
	command, err := reader.ReadCommand()
	if err != nil || command.Instructions() != "保留失败测试" || command.Kind() != protocol.CommandCompactContext {
		t.Fatal("UTF-8 command fragmented")
	}
	if _, err := reader.ReadCommand(); err != io.EOF {
		t.Fatal("reader did not drain")
	}
}

func TestAutomaticCompactionWireKeepsOriginalSubmitAssociation(t *testing.T) {
	sessionID := domain.SessionID("01900000-0000-7000-8000-000000000001")
	threadID := domain.ThreadID("01900000-0000-7000-8000-000000000002")
	id := domain.CompactionID("01900000-0000-7000-8000-000000000003")
	scope, _ := domain.NewAutoCompactionScope("01900000-0000-7000-8000-000000000004", 1)
	projector, err := NewStreamProjector(sessionID, threadID)
	if err != nil {
		t.Fatal(err)
	}
	started, err := protocol.NewCompactionStarted(sessionID, threadID, id, 2, scope)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := protocol.NewCompactionFailed(sessionID, threadID, id, protocol.CompactionFailedPayload{Scope: scope, Code: protocol.CompactionFailureResponse, Durable: true, UsageState: "unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	for _, event := range []protocol.Event{started, failed} {
		item, err := protocol.NewCorrelatedContextItem("original-submit", event)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := projector.Project(item)
		if err != nil {
			t.Fatal(err)
		}
		if err := NewStreamEncoder(&output).Write(wire); err != nil {
			t.Fatal(err)
		}
	}
	text := output.String()
	if strings.Count(text, `"request_id":"original-submit"`) != 2 || strings.Count(text, `"trigger":"auto"`) != 2 || strings.Contains(text, "control.response") || strings.Contains(text, "turn.failed") {
		t.Fatal("automatic wire association lost", text)
	}
}

package session

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestArtifactHistoryRequiresEarlierDurableReference(t *testing.T) {
	fixture := readCurrentFixture(t, "artifact_complete_tail.jsonl")
	var records []Record
	for _, line := range bytes.Split(bytes.TrimSpace(fixture), []byte{'\n'}) {
		r, err := DecodeRecord(line)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, r)
	}
	thread := records[0].ThreadID
	input, _ := tool.NewReadArtifactInput("0123456789abcdef0123456789abcdef", 0, 3)
	ready, _ := tool.NewReadArtifactReadyCall("read-artifact", input)
	id, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewReadArtifactInvocation(id, ready)
	page, _ := tool.NewArtifactPage(input.ID(), 0, 3, 10, true, "012")
	preview, _ := page.Preview()
	result, _ := tool.NewReadArtifactInvocationResult(invocation, tool.ResultSuccess, "ok", preview, page)
	draft, err := NewToolCallResultDraft(records[len(records)-1].TurnID, result)
	if err != nil {
		t.Fatal(err)
	}
	readRecord := Record{ThreadID: thread, PayloadVersion: EnvelopeVersion, EventKind: EventToolCallResult, Payload: draft.PayloadBytes()}
	if err := validateArtifactHistory(thread, append(append([]Record(nil), records...), readRecord)); err != nil {
		t.Fatal(err)
	}
	if err := validateArtifactHistory(thread, append([]Record{readRecord}, records...)); err == nil {
		t.Fatal("future reference authorized earlier read")
	}
	if err := validateArtifactHistory(thread, []Record{readRecord}); err == nil {
		t.Fatal("uncommitted object authorized read")
	}
	conflict := records[len(records)-1]
	conflict.Payload = []byte(strings.ReplaceAll(string(conflict.Payload), "01890f3e-7bcd-7abc-8abc-0123456789ad", "01890f3e-7bcd-7abc-8abc-0123456789ae"))
	if err := validateArtifactHistory(thread, append(append([]Record(nil), records...), conflict)); err == nil {
		t.Fatal("reused artifact ID accepted")
	}
}

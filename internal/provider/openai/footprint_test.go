package openai

import (
	"bytes"
	"encoding/json"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

func TestHistoryFootprintCountsCommittedVisibleAndOpaqueItems(t *testing.T) {
	t.Parallel()
	conversation := &Conversation{}
	turn := nativeTurn{
		User: NewUserItem("hello"),
		Outputs: []NativeItem{
			{Type: "reasoning", ID: "reason-1", ReasoningSummary: []ReasoningSummaryPart{{Type: "summary_text", Text: "private"}}, EncryptedContent: "opaque-encrypted"},
			{Type: "message", ID: "message-1", Role: "assistant", Phase: "final", Content: []ContentPart{{Type: "output_text", Text: "answer"}}},
			{Type: "future_item", Raw: json.RawMessage(`{"type":"future_item","opaque":{"value":2}}`)},
		},
	}
	conversation.history.commit(turn)
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		t.Fatal(err)
	}
	user, _ := json.Marshal(turn.User)
	bytesTotal := uint64(len(user))
	for _, item := range turn.Outputs {
		encoded, _ := json.Marshal(item)
		bytesTotal = estimate.SaturatingAdd(bytesTotal, uint64(len(encoded)))
	}
	want := estimate.ByteCount(bytesTotal)
	got, known := footprint.Estimate().Tokens()
	if footprint.Family() != domain.ProviderOpenAI || footprint.Revision() != 1 ||
		footprint.Estimate().Method() != estimate.MethodByteHeuristicV1 || !known || got != want {
		t.Fatalf("footprint = family %q revision %d estimate %#v", footprint.Family(), footprint.Revision(), footprint.Estimate())
	}
}

func TestHistoryFootprintAdvancesOnlyAfterPreparedFinalize(t *testing.T) {
	t.Parallel()
	conversation := &Conversation{}
	turn := validFootprintTurn()
	envelope, err := encodeNativeCommit(turn)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.NewPreparedSample(envelope, footprintUsage(t), func() { conversation.history.commit(turn) })
	if err != nil {
		t.Fatal(err)
	}
	before, _ := conversation.HistoryFootprint()
	if before.Revision() != 0 {
		t.Fatalf("staging revision = %d", before.Revision())
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	after, _ := conversation.HistoryFootprint()
	if after.Revision() != 1 {
		t.Fatalf("finalized revision = %d", after.Revision())
	}
	if err := prepared.Finalize(); err == nil {
		t.Fatal("duplicate finalize unexpectedly succeeded")
	}
	again, _ := conversation.HistoryFootprint()
	if again.Revision() != 1 {
		t.Fatalf("duplicate finalize advanced revision to %d", again.Revision())
	}
}

func TestHistoryFootprintDoesNotChangeNextResponsesRequest(t *testing.T) {
	t.Parallel()
	conversation := &Conversation{}
	conversation.history.commit(validFootprintTurn())
	next := NewUserItem("next")
	before, err := compileResponsesRequest("gpt-test", conversation.history.snapshot(), nil, next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conversation.HistoryFootprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := conversation.HistoryFootprint(); err != nil {
		t.Fatal(err)
	}
	after, err := compileResponsesRequest("gpt-test", conversation.history.snapshot(), nil, next)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Bytes(), after.Bytes()) {
		t.Fatalf("request changed after footprint:\n%s\n%s", before.Bytes(), after.Bytes())
	}
	first, _ := contextplan.NewSegment("request", contextplan.StabilityTurnStable, "v1", before)
	second, _ := contextplan.NewSegment("request", contextplan.StabilityTurnStable, "v1", after)
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("request fingerprint changed after footprint")
	}
}

func validFootprintTurn() nativeTurn {
	return nativeTurn{
		User: NewUserItem("hello"),
		Outputs: []NativeItem{{
			Type: "message", Role: "assistant", Phase: "final",
			Content: []ContentPart{{Type: "output_text", Text: "answer"}},
		}},
		Usage: rawUsage{},
	}
}

func footprintUsage(t *testing.T) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(1), domain.UnknownUsageMetric(), domain.UnknownUsageMetric(),
		domain.KnownUsageMetric(1), domain.UnknownUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

package anthropic

import (
	"bytes"
	"encoding/json"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

func TestHistoryFootprintCountsCommittedVisibleAndOpaqueMessages(t *testing.T) {
	t.Parallel()
	conversation := &Conversation{}
	entry := nativeHistoryEntry{
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("hello")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
			{Type: blockTypeThinking, Thinking: "private-thought", Signature: "opaque-signature"},
			{Type: blockTypeRedactedThinking, Raw: json.RawMessage(`{"type":"redacted_thinking","data":"opaque","future":true}`)},
			{Type: blockTypeText, Text: "answer"},
		}},
	}
	conversation.history.commit(entry)
	footprint, err := conversation.HistoryFootprint()
	if err != nil {
		t.Fatal(err)
	}
	user, _ := json.Marshal(entry.Input)
	assistant, _ := json.Marshal(entry.Assistant)
	want := estimate.ByteCount(uint64(len(user) + len(assistant)))
	got, known := footprint.Estimate().Tokens()
	if footprint.Family() != domain.ProviderAnthropic || footprint.Revision() != 1 ||
		footprint.Estimate().Method() != estimate.MethodByteHeuristic || !known || got != want {
		t.Fatalf("footprint = family %q revision %d estimate %#v", footprint.Family(), footprint.Revision(), footprint.Estimate())
	}
}

func TestHistoryFootprintAdvancesOnlyAfterPreparedFinalize(t *testing.T) {
	t.Parallel()
	conversation := &Conversation{}
	entry := validFootprintEntry()
	envelope, err := encodeNativeCommit(entry)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := provider.NewPreparedSample(envelope, footprintUsage(t), func() { conversation.history.commit(entry) })
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

func TestHistoryFootprintDoesNotChangeNextMessagesRequest(t *testing.T) {
	t.Parallel()
	conversation := &Conversation{}
	conversation.history.commit(validFootprintEntry())
	next := newUserMessage("next")
	before, err := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, conversation.history.snapshot(), nil, nativeMessagePointer(next), testAnthropicToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conversation.HistoryFootprint(); err != nil {
		t.Fatal(err)
	}
	if _, err := conversation.HistoryFootprint(); err != nil {
		t.Fatal(err)
	}
	after, err := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, conversation.history.snapshot(), nil, nativeMessagePointer(next), testAnthropicToolView(t))
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

func validFootprintEntry() nativeHistoryEntry {
	return nativeHistoryEntry{
		Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("hello")),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeText, Text: "answer"}}},
		Metadata:  messageMetadata{ID: "msg-1", Model: "claude-test"},
	}
}

func footprintUsage(t *testing.T) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(1), domain.UnknownUsageMetric(), domain.NotApplicableUsageMetric(),
		domain.KnownUsageMetric(1), domain.NotApplicableUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

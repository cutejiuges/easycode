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

func TestPlanReviewControlPreservesProposalAndRejectsUnrelatedPayload(t *testing.T) {
	ref := PlanReviewRef{SessionID: testSessionID, ThreadID: testThreadID, Sample: domain.AssistantSampleReference{TurnID: testTurnID, SampleIndex: 2}}
	const body = "  # 方案\r\n\t@literal\n"
	item, err := NewPlanReviewOfferItem(testRequestID, ref, body)
	if err != nil {
		t.Fatal(err)
	}
	offer, ok := item.PlanReviewOffer()
	if !ok || offer.Ref != ref || offer.Markdown != body {
		t.Fatal("proposal identity or exact text was lost")
	}
	event, _ := item.ContextEvent()
	if _, err := NewCorrelatedTurnItem(testRequestID, event); err == nil {
		t.Fatal("review leaked into the live turn stream")
	}
	for _, payload := range []string{
		`{"markdown":"plan"}`,
		`{"sample_index":16,"markdown":"plan"}`,
		`{"sample_index":2,"markdown":"plan","extra":true}`,
		`{"sample_index":2,"sample_index":3,"markdown":"plan"}`,
	} {
		bad := event
		bad.Payload = []byte(payload)
		if _, err := NewCorrelatedContextItem(testRequestID, bad); err == nil {
			t.Fatalf("unexpected valid plan offer: %s", payload)
		}
	}
	event.ThreadID = ""
	if _, err := NewCorrelatedContextItem(testRequestID, event); err == nil {
		t.Fatal("review offer accepted without a thread")
	}
	command, err := NewApprovePlanCommand(testRequestID, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Kind().ValidateAcceptance(CommandDispositionQueued, "", ""); err == nil {
		t.Fatal("plan approval accepted without a starting input")
	}
	command.text = body
	if err := command.Validate(); err == nil {
		t.Fatal("plan approval accepted replacement text")
	}
}

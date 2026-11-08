package headless

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

func headlessReasoning(t *testing.T, closed bool) []protocol.Event {
	t.Helper()
	start, _ := protocol.NewReasoningStarted(0, domain.ReasoningOpenAISummary)
	delta, _ := protocol.NewReasoningDelta(0, domain.ReasoningOpenAISummary, 0, "reasoning-sentinel")
	events := []protocol.Event{runtimeStarted(), decorateRuntime(start), decorateRuntime(delta)}
	if closed {
		complete, _ := protocol.NewReasoningCompleted(0, domain.ReasoningOpenAISummary, protocol.ReasoningCompletion{Availability: domain.ReasoningReadable, TotalBytes: 18, Text: "reasoning-sentinel"})
		presentation, _ := domain.NewSamplePresentation(1, []domain.ReasoningBinding{{Block: 0, Item: 0, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}})
		commit, _ := protocol.NewSampleTranscriptCommitted(9, 0, presentation)
		events = append(events, decorateRuntime(complete), decorateRuntime(commit), runtimeDelta(t, "answer"), runtimeCompleted())
	} else {
		failed, err := protocol.NewTurnSampleFailed(string(fault.CodeUserCancelled), "turn was cancelled", true, protocol.NewUncommittedSampleResolution(0))
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, decorateRuntime(failed))
	}
	return events
}
func TestHeadlessReasoningIsValidatedAndNeverOutput(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		for _, closed := range []bool{false, true} {
			var output bytes.Buffer
			config := textConfig(&output)
			if jsonMode {
				config = jsonConfig(&output, false)
			}
			result := Run(context.Background(), fixedSession(headlessReasoning(t, closed)...), config)
			if result.Completed != closed || !closed && result.Failure.Code != fault.CodeUserCancelled {
				t.Fatal(result)
			}
			for _, secret := range []string{"reasoning-sentinel", "sample_index", "bindings", "not_committed", "reasoning.completed"} {
				if strings.Contains(output.String(), secret) {
					t.Fatal("internal event leaked", output.String())
				}
			}
			if closed && !strings.Contains(output.String(), "answer") {
				t.Fatal("normal answer missing")
			}
		}
	}
}
func TestHeadlessReasoningRejectsMalformedUnresolvedAndLateEvents(t *testing.T) {
	valid := headlessReasoning(t, true)
	tests := [][]protocol.Event{
		append(append([]protocol.Event{}, valid[:3]...), runtimeCompleted()),
		append(append([]protocol.Event{}, valid...), valid[1]),
		{runtimeStarted(), valid[2]},
		{runtimeStarted(), valid[1], runtimeFailure(t, fault.Summary{Code: fault.CodeProviderRequest, Message: "provider request failed"})},
	}
	for _, events := range tests {
		var output bytes.Buffer
		result := Run(context.Background(), fixedSession(events...), jsonConfig(&output, false))
		if result.Failure.Code != fault.CodeStreamProtocol {
			t.Fatal("invalid lifecycle accepted", result, output.String())
		}
	}
}

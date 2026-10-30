package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
	"testing"
)

type controlledToolConversation struct{ *controlledConversation }

func (conversation *controlledToolConversation) PrepareToolOutputs(results []tool.InvocationResult) (*provider.PreparedToolOutputs, error) {
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 2, []byte(`{"kind":"tool_outputs"}`))
	if err != nil {
		return nil, err
	}
	return provider.NewPreparedToolOutputs(envelope, func() {}, func() {})
}

func collectControlOutput(loop *AgentLoop) <-chan []protocol.ControlItem {
	done := make(chan []protocol.ControlItem, 1)
	go func() {
		var items []protocol.ControlItem
		for item := range loop.Outputs() {
			items = append(items, item)
		}
		done <- items
	}()
	return done
}
func appendAutomatic(t *testing.T, loop *AgentLoop, requestID protocol.RequestID, text string) protocol.CommandResult {
	t.Helper()
	command, err := protocol.NewSubmitConversationalInputCommand(requestID, text)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Submit(context.Background(), command)
	if err != nil || result.Status() != protocol.CommandStatusAccepted || !result.EntryID().Valid() {
		t.Fatalf("automatic admission: %#v %v", result, err)
	}
	return result
}

func TestAgentLoopSteersOnlyCompleteToolBoundaryAndPreservesStrictFIFO(t *testing.T) {
	conversation := &controlledToolConversation{newControlledConversation()}
	journal := &fakeJournal{}
	loop := newControlledAgentLoop(t, conversation, journal)
	outputs := collectControlOutput(loop)
	done := startAgentLoop(t, loop)
	first := appendAutomatic(t, loop, "auto-1", "first")
	call1 := receiveInvocation(t, conversation.calls)
	second := appendAutomatic(t, loop, "auto-2", " second\n")
	third := appendAutomatic(t, loop, "auto-3", "third")
	submitLoopCommand(t, loop, "strict-4", "strict")
	fifth := appendAutomatic(t, loop, "auto-5", "fifth")
	call1.release <- completedProviderEventWithSample(t, newPreparedSampleWithReady(t, testRuntimeReadyCall(t, "call-read")))
	call2 := receiveInvocation(t, conversation.calls)
	if len(call2.history.Turns) != 1 || len(call2.history.Turns[0].Messages) != 3 || call2.history.Turns[0].Messages[1].InputID != second.EntryID() || call2.history.Turns[0].Messages[2].InputID != third.EntryID() || call2.history.Turns[0].Messages[1].Text != " second\n" {
		t.Fatal("safe point did not consume only the automatic prefix")
	}
	call2.release <- completedProviderEvent(t)
	call3 := receiveInvocation(t, conversation.calls)
	if call3.input != "strict" || len(call3.history.Turns) != 2 || len(call3.history.Turns[1].Messages) != 1 {
		t.Fatal("strict barrier was crossed")
	}
	call3.release <- completedProviderEvent(t)
	call4 := receiveInvocation(t, conversation.calls)
	if call4.input != "fifth" || len(call4.history.Turns) != 3 {
		t.Fatal("input after final sample did not start next turn")
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	call4.release <- completedProviderEvent(t)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	items := <-outputs
	for _, result := range []protocol.CommandResult{first, second, third, fifth} {
		accepted, claimed, committed := -1, -1, -1
		for i, item := range items {
			if r, ok := item.CommandResult(); ok && r.RequestID() == result.RequestID() {
				accepted = i
			}
			if status, ok := item.InputStatus(); ok && status.EntryID() == result.EntryID() {
				switch status.State() {
				case protocol.InputClaimed:
					if claimed >= 0 {
						t.Fatal("duplicate claim")
					}
					claimed = i
				case protocol.InputCommitted:
					if committed >= 0 {
						t.Fatal("duplicate commit")
					}
					committed = i
				default:
					t.Fatalf("unexpected resolution: %s", status.State())
				}
			}
		}
		if accepted < 0 || claimed <= accepted || committed <= claimed {
			t.Fatal("input state order changed")
		}
	}
	var steeringIndex, outputIndex int
	for i, batch := range journal.snapshot() {
		if len(batch) == 1 && batch[0].EventKind() == session.EventProviderNativeCommit {
			outputIndex = i
		}
		if batch[0].EventKind() == session.EventTurnInputsCommitted {
			record := session.Record{PayloadVersion: session.EnvelopeVersion, TurnID: batch[0].TurnID(), EventKind: batch[0].EventKind(), Payload: batch[0].PayloadBytes()}
			payload, err := session.DecodeTurnInputsPayload(record)
			if err != nil {
				t.Fatal(err)
			}
			if payload.Placement == domain.InputSteering {
				steeringIndex = i
				if payload.TargetSampleIndex != 1 || len(payload.InputIDs) != 2 {
					t.Fatal("steering batch association changed")
				}
			}
		}
	}
	if outputIndex == 0 || steeringIndex != outputIndex+1 {
		t.Fatal("steering was not committed immediately after full native tool outputs")
	}
}

func TestAgentLoopRetractStopAndStrictNextHaveOneResolution(t *testing.T) {
	releaseCleanup := make(chan struct{})
	conversation := newControlledConversation()
	conversation.cancelRelease = releaseCleanup
	loop := newControlledAgentLoop(t, conversation, &fakeJournal{})
	outputs := collectControlOutput(loop)
	done := startAgentLoop(t, loop)
	appendAutomatic(t, loop, "running", "first")
	first := receiveInvocation(t, conversation.calls)
	retractable := appendAutomatic(t, loop, "edit", "edit me")
	returned := appendAutomatic(t, loop, "return", "return me")
	submitLoopCommand(t, loop, "strict", "next")
	retract, _ := protocol.NewRetractInputsCommand("retract", []domain.InputID{retractable.EntryID()})
	result, err := loop.Submit(context.Background(), retract)
	if err != nil || result.Disposition() != protocol.CommandDispositionRetracted {
		t.Fatalf("retract: %#v %v", result, err)
	}
	stale, _ := protocol.NewRetractInputsCommand("stale", []domain.InputID{retractable.EntryID(), returned.EntryID()})
	result, err = loop.Submit(context.Background(), stale)
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, result, protocol.ControlErrorInputNotQueued)
	stop, _ := protocol.NewStopTurnCommand("stop", first.history.Turns[0].TurnID)
	result, err = loop.Submit(context.Background(), stop)
	if err != nil || result.Disposition() != protocol.CommandDispositionStopping {
		t.Fatalf("stop: %#v %v", result, err)
	}
	blocked, _ := protocol.NewSubmitConversationalInputCommand("blocked", "must remain draft")
	result, err = loop.Submit(context.Background(), blocked)
	if err != nil {
		t.Fatal(err)
	}
	assertRejectedCode(t, result, protocol.ControlErrorInputStopping)
	<-first.ctx.Done()
	close(releaseCleanup)
	next := receiveInvocation(t, conversation.calls)
	if next.input != "next" {
		t.Fatal("stop discarded strict-next input")
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	next.release <- completedProviderEvent(t)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	items := <-outputs
	for _, expected := range []struct {
		id     domain.InputID
		reason protocol.InputReason
	}{{retractable.EntryID(), protocol.InputReasonRetracted}, {returned.EntryID(), protocol.InputReasonStopped}} {
		count := 0
		for _, item := range items {
			if status, ok := item.InputStatus(); ok && status.EntryID() == expected.id {
				count++
				if status.State() != protocol.InputReturned || status.Reason() != expected.reason {
					t.Fatal("input unexpectedly claimed or resolved")
				}
			}
		}
		if count != 1 {
			t.Fatalf("input resolution count=%d", count)
		}
	}
}

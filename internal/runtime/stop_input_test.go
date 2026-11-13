package runtime

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestStopUsesUniqueInputOwnerAndPreservesStrictFIFO(t *testing.T) {
	r, runner, requests := newStopRuntimeFixture(t, &fakeJournal{})
	r.newTurnID = sequentialTurnIDs()
	original := runner.run
	runner.run = func(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration) domain.HookExecution {
		if len(runner.inputs) > 1 {
			return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}}
		}
		return original(ctx, id, d)
	}
	loop, err := NewAgentLoop(r, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	type trustRequest struct {
		turn    domain.TurnID
		payload protocol.HookTrustRequestedPayload
	}
	trusts := make(chan trustRequest, 1)
	collected := make(chan []protocol.ControlItem, 1)
	go func() {
		var items []protocol.ControlItem
		for item := range loop.Outputs() {
			items = append(items, item)
			if event, ok := item.TurnEvent(); ok && event.Kind == protocol.EventHookTrustRequested {
				p, _ := protocol.DecodeHookTrustRequested(event)
				trusts <- trustRequest{event.TurnID, p}
			}
		}
		collected <- items
	}()
	done := startAgentLoop(t, loop)
	first := appendAutomatic(t, loop, "first", "first task")
	request := <-trusts
	second := appendAutomatic(t, loop, "second", "  correction\n")
	third := appendAutomatic(t, loop, "third", "retracted")
	strict := submitLoopCommand(t, loop, "strict", "strict-next")
	if strict.Status() != protocol.CommandStatusAccepted {
		t.Fatal("strict input rejected")
	}
	fifth := appendAutomatic(t, loop, "fifth", "after barrier")
	retract, _ := protocol.NewRetractInputsCommand("retract-third", []domain.InputID{third.EntryID()})
	if result, err := loop.Submit(context.Background(), retract); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("retract failed", err)
	}
	response, _ := protocol.NewHookTrustResponse(r.sessionID, r.threadID, request.turn, request.payload, protocol.HookTrustSession)
	command, _ := protocol.NewResolveHookTrustCommand("trust", response)
	if result, err := loop.Submit(context.Background(), command); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal(err)
	}
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	items := <-collected
	history := r.conversation.ProjectHistory()
	if len(history.Turns) != 3 || requests.Load() != 4 || len(history.Turns[0].Messages) != 5 {
		t.Fatalf("invalid continuation/strict split: turns=%d requests=%d history=%#v", len(history.Turns), requests.Load(), history)
	}
	firstMessages := history.Turns[0].Messages
	if firstMessages[2].Role != domain.SemanticHook || firstMessages[3].InputID != second.EntryID() || firstMessages[3].Text != "  correction\n" || history.Turns[1].Messages[0].Text != "strict-next" || history.Turns[2].Messages[0].InputID != fifth.EntryID() {
		t.Fatal("FIFO barrier or real input provenance changed")
	}
	for _, id := range []domain.InputID{first.EntryID(), second.EntryID(), fifth.EntryID()} {
		count := 0
		for _, item := range items {
			if status, ok := item.InputStatus(); ok && status.EntryID() == id && status.State() == protocol.InputCommitted {
				count++
			}
		}
		if count != 1 {
			t.Fatal("duplicate or missing committed status", id, count)
		}
	}
	strictCommits := 0
	for _, item := range items {
		if event, ok := item.TurnEvent(); ok && event.Kind == protocol.EventUserInputCommitted {
			input, _ := protocol.DecodeUserInputCommitted(event)
			if input.InputID == history.Turns[1].Messages[0].InputID {
				strictCommits++
			}
		}
	}
	if strictCommits != 1 {
		t.Fatal("strict input not committed exactly once")
	}
	for _, turn := range history.Turns {
		for _, message := range turn.Messages {
			if message.InputID == third.EntryID() {
				t.Fatal("retracted input entered history")
			}
		}
	}
}

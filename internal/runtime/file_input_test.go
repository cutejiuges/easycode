package runtime

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func runtimeFileInputs(t *testing.T, text string) []domain.UserInput {
	t.Helper()
	ref, err := domain.NewFileReference("file", domain.NewWholeFileSelection())
	if err != nil {
		t.Fatal(err)
	}
	a, err := domain.FreezeFileAttachment(ref, "FROZEN_RUNTIME\n")
	if err != nil {
		t.Fatal(err)
	}
	files, err := domain.NewFileAttachments([]domain.FileAttachment{a})
	if err != nil {
		t.Fatal(err)
	}
	input, err := domain.NewUserInput("01900000-0000-7000-8000-000000000001", text)
	if err != nil {
		t.Fatal(err)
	}
	input, err = input.WithFileAttachments(files)
	if err != nil {
		t.Fatal(err)
	}
	return []domain.UserInput{input}
}

func TestFileQueueChargesMetadataAndKeepsFrozenValueThroughClaim(t *testing.T) {
	c := newControlledConversation()
	r := newTestRuntime(t, c, &fakeJournal{})
	config := DefaultAgentLoopConfig()
	input := runtimeFileInputs(t, "next @file")[0]
	config.MaxQueuedBytes = input.ByteSize()
	loop, err := NewAgentLoop(r, config)
	if err != nil {
		t.Fatal(err)
	}
	done := startAgentLoop(t, loop)
	submitLoopCommand(t, loop, "seed", "seed")
	first := receiveInvocation(t, c.calls)
	command, _ := protocol.NewSubmitConversationalInputCommand("file-queue", input.Text())
	command, err = command.WithFileAttachments(input.FileAttachments())
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Submit(context.Background(), command)
	if err != nil || result.Disposition() != protocol.CommandDispositionQueued {
		t.Fatal("file did not queue", err)
	}
	overflow := submitLoopCommand(t, loop, "file-overflow", "x")
	assertRejectedCode(t, overflow, protocol.ControlErrorInputQueueFull)
	collected := make(chan []protocol.ControlItem, 1)
	go func() { collected <- drainControlItems(loop.Outputs()) }()
	first.release <- completedProviderEvent(t)
	next := receiveInvocation(t, c.calls)
	if err := loop.CloseInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	next.release <- completedProviderEvent(t)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	committed := 0
	for _, item := range <-collected {
		if event, ok := item.TurnEvent(); ok && event.Kind == protocol.EventUserInputCommitted {
			p, err := protocol.DecodeUserInputCommitted(event)
			if err != nil {
				t.Fatal(err)
			}
			if p.InputID == result.EntryID() {
				committed++
				if p.Text != input.Text() || p.Files.Total() != 1 {
					t.Fatal("claim lost frozen files")
				}
			}
		}
	}
	if committed != 1 {
		t.Fatal("file input committed repeatedly or disappeared", committed)
	}
}

func TestFileMetadataDoesNotHideNativeEscapeExpansion(t *testing.T) {
	input := runtimeFileInputs(t, "inspect @file "+strings.Repeat("\x01", domain.MaxUserInputBytes-4096))
	r := newTestRuntime(t, &fakeConversation{stream: fixedStream()}, &fakeJournal{})
	a, _ := inputAssociation(runtimeTurnID, 0, input)
	if r.inputEncodingFits(a, input) {
		t.Fatal("native/record escape expansion reached writer")
	}
}

func submitFileLoopCommand(t *testing.T, loop *AgentLoop, id protocol.RequestID, text string) protocol.CommandResult {
	t.Helper()
	input := runtimeFileInputs(t, text)[0]
	c, err := protocol.NewSubmitConversationalInputCommand(id, text)
	if err != nil {
		t.Fatal(err)
	}
	c, err = c.WithFileAttachments(input.FileAttachments())
	if err != nil {
		t.Fatal(err)
	}
	result, err := loop.Submit(context.Background(), c)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("file command rejected", err)
	}
	return result
}

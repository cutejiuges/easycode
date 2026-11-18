package runtime

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestInputEventFailuresRejectBeforeWriterAdmission(t *testing.T) {
	for _, scenario := range []string{"constructor", "invalid_payload", "wrong_input", "wrong_kind", "wrong_summary"} {
		t.Run(scenario, func(t *testing.T) {
			conversation := &fakeConversation{stream: fixedStream()}
			journal := &fakeJournal{}
			r := newTestRuntime(t, conversation, journal)
			input := runtimeInputs("question @a")[0]
			ref, _ := domain.NewFileReference("a", domain.NewWholeFileSelection())
			attachment, _ := domain.FreezeFileAttachment(ref, "private-file-body\n")
			files, _ := domain.NewFileAttachments([]domain.FileAttachment{attachment})
			input, _ = input.WithFileAttachments(files)
			r.userInputCommitted = func(value domain.UserInput) (protocol.Event, error) {
				switch scenario {
				case "constructor":
					return protocol.Event{}, errors.New("injected view failure")
				case "wrong_input":
					return protocol.NewUserInputCommitted(runtimeInputs("different")[0])
				case "wrong_kind":
					return protocol.NewAssistantTextDelta("wrong event")
				case "wrong_summary":
					plain, _ := domain.NewUserInput(value.ID(), value.Text())
					return protocol.NewUserInputCommitted(plain)
				default:
					event, _ := protocol.NewUserInputCommitted(value)
					event.Payload = []byte(`{}`)
					return event, nil
				}
			}
			a, _ := inputAssociation(runtimeTurnID, 0, []domain.UserInput{input})
			if r.inputEncodingFits(a, []domain.UserInput{input}) {
				t.Fatal("admission ignored mandatory view failure")
			}
			result := r.commitInputs(context.Background(), runtimeTurnID, 0, []domain.UserInput{input}, nil, func(protocol.Event) { t.Fatal("uncommitted event emitted") })
			if result.err == nil || !result.preflightRejected || result.committed || journal.calls != 0 || r.poisoned.Load() || len(conversation.ProjectHistory().Turns) != 0 || conversation.calls.Load() != 0 {
				t.Fatal("view failure wrote, finalized or poisoned", result.err)
			}
		})
	}
}

package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"testing"
)

type inputResolutionRecorder struct {
	states  []protocol.InputState
	reasons []protocol.InputReason
}

func (r *inputResolutionRecorder) claim(context.Context, domain.TurnID, uint32) ([]domain.UserInput, error) {
	return nil, nil
}
func (r *inputResolutionRecorder) resolve(_ domain.TurnID, _ []domain.UserInput, state protocol.InputState, reason protocol.InputReason) {
	r.states = append(r.states, state)
	r.reasons = append(r.reasons, reason)
}

func TestInputCommitCancellationLinearization(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		before, poison, fail bool
	}{
		{name: "cancel before admission", before: true}, {name: "cancel after admission"}, {name: "uncertain sync", fail: true, poison: true}, {name: "definite rejection", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			journal := &startedFailureJournal{}
			if tc.fail {
				journal.failAt = 1
				journal.poisonOnFailure = tc.poison
			}
			conversation := &fakeConversation{stream: fixedStream()}
			var target Journal = journal
			if !tc.fail {
				target = &fakeJournal{hook: func([]session.RecordDraft) { cancel() }}
			}
			runtime := newTestRuntime(t, conversation, target)
			if tc.before {
				cancel()
			}
			recorder := &inputResolutionRecorder{}
			var events []protocol.Event
			got := runtime.commitInputs(ctx, runtimeTurnID, 0, runtimeFileInputs(t, "  durable input @file\n"), recorder, func(e protocol.Event) { events = append(events, e) })
			want := protocol.InputCommitted
			if tc.before || (tc.fail && !tc.poison) {
				want = protocol.InputReturned
			}
			if tc.poison {
				want = protocol.InputUncertain
			}
			if len(recorder.states) != 1 || recorder.states[0] != want {
				t.Fatalf("resolution: %#v", recorder.states)
			}
			if got.committed != (want == protocol.InputCommitted) {
				t.Fatalf("commit=%t error=%v", got.committed, got.err)
			}
			if want == protocol.InputCommitted {
				if len(events) != 1 || events[0].Kind != protocol.EventUserInputCommitted || len(conversation.ProjectHistory().Turns) != 1 {
					t.Fatal("durable cancelled input was lost")
				}
			} else if len(events) != 0 || len(conversation.ProjectHistory().Turns) != 0 {
				t.Fatal("uncommitted input became visible")
			}
			if tc.poison && !runtime.poisoned.Load() {
				t.Fatal("uncertain persistence did not poison runtime")
			}
		})
	}
}

package runtime

import (
	"context"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"fmt"
	"strings"
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
			got := runtime.commitInputs(ctx, runtimeTurnID, 0, runtimeInputs("  durable input\n"), recorder, func(e protocol.Event) { events = append(events, e) })
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

func TestInputEncodingBudgetSplitsMaximalPrefixWithoutWriting(t *testing.T) {
	journal := &fakeJournal{}
	runtime := newTestRuntime(t, &fakeConversation{stream: fixedStream()}, journal)
	huge := runtimeInputs(strings.Repeat("\x01", domain.MaxUserInputBytes))
	a, _ := inputAssociation(runtimeTurnID, 0, huge)
	if runtime.inputEncodingFits(a, huge) {
		t.Fatal("escaped oversized input accepted")
	}
	var queue []queuedInput
	for i := 0; i < 3; i++ {
		id := domain.InputID(fmt.Sprintf("01900000-0000-7000-8000-%012d", i+1))
		value, _ := domain.NewUserInput(id, strings.Repeat("\x01", 1<<20))
		queue = append(queue, queuedInput{value: value, text: value.Text(), automatic: true})
	}
	loop, err := NewAgentLoop(runtime, DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	state := newAgentLoopRunState(loop, context.Background())
	state.queue = queue
	state.queuedBytes = 3 << 20
	claimed := state.takePrefix(runtimeTurnID, 0, true)
	if len(claimed) != 2 || len(state.queue) != 1 || state.queue[0].value.ID() != "01900000-0000-7000-8000-000000000003" || state.queuedBytes != 3<<20 {
		t.Fatal("encoding prefix split or unresolved quota changed")
	}
	if len(journal.snapshot()) != 0 || journal.Poisoned() {
		t.Fatal("preflight reached writer")
	}
}

func TestPreflightReturnsLongInputAndAllowsShortRetry(t *testing.T) {
	conversation := &fakeConversation{stream: fixedStream()}
	journal := &fakeJournal{}
	cfg := testRuntimeConfig(t, journal)
	budget, err := contextplan.NewBudget(100000, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ContextBudget = budget
	runtime, err := New(conversation, cfg)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &inputResolutionRecorder{}
	emit := func(protocol.Event) {}
	rejected := runtime.commitInputs(context.Background(), runtimeTurnID, 0, runtimeInputs(strings.Repeat("x", 1<<20)), recorder, emit)
	if !rejected.preflightRejected || len(journal.snapshot()) != 0 || len(conversation.ProjectHistory().Turns) != 0 || len(recorder.states) != 1 || recorder.states[0] != protocol.InputReturned || recorder.reasons[0] != protocol.InputReasonPreflight {
		t.Fatal("over-limit input was retained")
	}
	accepted := runtime.commitInputs(context.Background(), runtimeTurnID, 0, runtimeInputs("short retry"), recorder, emit)
	if !accepted.committed || len(journal.snapshot()) != 1 || len(conversation.ProjectHistory().Turns) != 1 || recorder.states[1] != protocol.InputCommitted {
		t.Fatalf("shortened retry failed: %v", accepted.err)
	}
	if conversation.calls.Load() != 0 || journal.Poisoned() {
		t.Fatal("preflight performed network or poisoned writer")
	}
}

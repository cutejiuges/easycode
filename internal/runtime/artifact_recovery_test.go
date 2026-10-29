package runtime

import (
	"context"
	"testing"

	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestReadArtifactRecoveryNeverReopensSavedOutput(t *testing.T) {
	for _, state := range []session.ReplayedToolCallState{session.ReplayedToolCallReady, session.ReplayedToolCallStarted, session.ReplayedToolCallResult} {
		t.Run(string(state), func(t *testing.T) {
			id, _ := tool.GenerateInvocationID()
			input, _ := tool.NewReadArtifactInput("0123456789abcdef0123456789abcdef", 0, 3)
			ready, _ := tool.NewReadArtifactReadyCall("artifact", input)
			invocation, _ := tool.NewInvocation(id, ready)
			call := session.ReplayedToolCall{Invocation: invocation, State: state, ReadySequence: 2}
			if state != session.ReplayedToolCallReady {
				call.StartedSequence = 3
			}
			if state == session.ReplayedToolCallResult {
				call.ResultSequence = 4
				page, _ := tool.NewArtifactPage(input.ID(), 0, 3, 3, true, "abc")
				preview, _ := page.Preview()
				v, _ := invocation.ReadArtifact()
				call.Result, _ = tool.NewReadArtifactInvocationResult(v, tool.ResultSuccess, "ok", preview, page)
			}
			plan, err := session.NewToolRecoveryPlan(runtimeTurnID, 1, []session.ReplayedToolCall{call}, false)
			if err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			close(release)
			executor := &gatedArtifactExecutor{entered: make(chan tool.InvocationID, 1), release: release, calls: make(map[tool.InvocationID]int)}
			conversation := &fakeToolConversation{fakeConversation: &fakeConversation{}}
			config := testRuntimeConfig(t, &fakeJournal{})
			config.ReadArtifactExecutor = executor
			runtime, err := New(conversation, config)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.ReconcileToolTurn(context.Background(), plan); err != nil {
				t.Fatal(err)
			}
			if len(executor.calls) != 0 || len(conversation.results) != 1 {
				t.Fatal("recovery reopened output or lost pairing")
			}
			result := conversation.results[0]
			want := "session_interrupted_before_execution"
			if state == session.ReplayedToolCallStarted {
				want = "outcome_uncertain"
			}
			if state == session.ReplayedToolCallResult {
				want = "ok"
				if result.Preview() != call.Result.Preview() {
					t.Fatal("frozen artifact page changed")
				}
			}
			if result.Code() != want || result.InvocationID() != id || len(runtime.readObservations.current) != 0 {
				t.Fatal("recovery changed artifact semantics")
			}
		})
	}
}

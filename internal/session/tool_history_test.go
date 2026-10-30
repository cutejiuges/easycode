package session

import (
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestToolHistoryProjectsAllTurnsAndTerminalStates(t *testing.T) {
	states := []tool.ResultStatus{tool.ResultSuccess, tool.ResultDenied, tool.ResultCancelled, tool.ResultOutcomeUncertain}
	var records []Record
	for i, status := range states {
		turn, _ := domain.GenerateTurnID()
		id, _ := tool.GenerateInvocationID()
		input, _ := tool.NewExecInput(fmt.Sprintf("printf command-%d", i), "", 0)
		ready, _ := tool.NewExecReadyCall(tool.ProviderCallID(fmt.Sprintf("call-%d", i)), input)
		invocation, _ := tool.NewInvocation(id, ready)
		call, err := NewToolCallReadyDraft(turn, invocation, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		exec, _ := invocation.Exec()
		result := tool.NewExecErrorResult(exec, status, "fixture_result", fmt.Sprintf("frozen-%d", i))
		if status == tool.ResultSuccess {
			meta, _ := tool.NewExecResultMetadata("", 2, true, 1, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
			preview, _ := tool.NewModelPreview(fmt.Sprintf("frozen-%d", i))
			result, err = tool.NewExecInvocationResult(exec, status, "ok", preview, meta)
			if err != nil {
				t.Fatal(err)
			}
		}
		terminal, err := NewToolCallResultDraft(turn, result)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, Record{Sequence: uint64(3*i + 1), TurnID: turn, EventKind: EventProviderNativeCommit}, Record{PayloadVersion: EnvelopeVersion, Sequence: uint64(3*i + 2), TurnID: turn, EventKind: EventToolCallReady, Payload: call.PayloadBytes()}, Record{PayloadVersion: EnvelopeVersion, Sequence: uint64(3*i + 3), TurnID: turn, EventKind: EventToolCallResult, Payload: terminal.PayloadBytes()})
	}
	commands, err := projectToolHistory(records)
	if err != nil || len(commands) != len(states) {
		t.Fatalf("projection: %v", err)
	}
	for i, command := range commands {
		if command.NativeSequence != uint64(3*i+1) || command.Sequence != uint64(3*i+2) || !command.HasResult || command.Result.Status() != states[i] || command.Result.Preview().Text() != fmt.Sprintf("frozen-%d", i) {
			t.Fatal("history lost ordering or a terminal state")
		}
	}
	commands[0] = ToolHistoryEntry{}
	again, err := projectToolHistory(records)
	if err != nil || again[0].Result.Status() != tool.ResultSuccess {
		t.Fatal("projection mutation changed facts")
	}
}

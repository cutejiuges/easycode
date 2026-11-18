package session

import (
	"testing"

	"easycode/internal/tool"
)

func replayExecInvocation(t *testing.T) tool.Invocation {
	t.Helper()
	input, _ := tool.NewExecInput("printf hello", "src", 1000)
	ready, _ := tool.NewExecReadyCall("call-exec", input)
	id, _ := tool.ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789ad")
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func replayExecResult(t *testing.T, invocation tool.Invocation, status tool.ResultStatus, code string) RecordDraft {
	t.Helper()
	exec, _ := invocation.Exec()
	result := tool.NewExecErrorResult(exec, status, code, "frozen Exec output")
	if status == tool.ResultSuccess {
		metadata, _ := tool.NewExecResultMetadata("src", 2, true, 123, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
		preview, _ := tool.NewModelPreview("frozen Exec output")
		var err error
		result, err = tool.NewExecInvocationResult(exec, status, code, preview, metadata)
		if err != nil {
			t.Fatal(err)
		}
	}
	draft, err := NewToolCallResultDraft(testTurnID, result)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

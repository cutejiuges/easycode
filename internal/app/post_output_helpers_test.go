package app

import (
	"easycode/internal/session"
	"easycode/internal/tool"
	"testing"
)

func testModelOutputs(t *testing.T, call session.ReplayedToolCall, results ...tool.InvocationResult) []tool.ModelOutput {
	t.Helper()
	out := make([]tool.ModelOutput, len(results))
	for i, result := range results {
		copy := call
		copy.Result = result
		evidence, err := copy.InputResultEvidence()
		if err != nil {
			t.Fatal(err)
		}
		v, err := tool.NewModelOutput(result, evidence)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = v
	}
	return out
}

func mustEntryInputEvidence(t *testing.T, entry session.ToolHistoryEntry) tool.InputResultEvidence {
	t.Helper()
	evidence, err := entry.InputResultEvidence()
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

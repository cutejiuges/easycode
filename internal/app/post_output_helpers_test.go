package app

import (
	"easycode/internal/tool"
	"testing"
)

func testModelOutputs(t *testing.T, results ...tool.InvocationResult) []tool.ModelOutput {
	t.Helper()
	out := make([]tool.ModelOutput, len(results))
	for i, result := range results {
		v, err := tool.NewModelOutput(result, nil)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = v
	}
	return out
}

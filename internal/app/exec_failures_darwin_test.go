//go:build darwin

package app

import (
	"context"

	"easycode/internal/tool"
)

type spawnFailureExec struct{ calls int }

func (executor *spawnFailureExec) Execute(_ context.Context, candidate tool.ExecCandidate, _ func(tool.ExecProgress)) tool.InvocationResult {
	executor.calls++
	return tool.NewExecErrorResult(candidate.Invocation(), tool.ResultError, "process_start_failed", "Exec failed: process_start_failed")
}

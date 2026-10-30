package builtin

import (
	"context"
	"easycode/internal/tool"
)

// testCommandOutputStore 在既有执行器测试中显式模拟存储不可用。
type testCommandOutputStore struct{}

func (testCommandOutputStore) StartCapture(context.Context, tool.InvocationID) (tool.CommandOutputCapture, error) {
	return nil, tool.ArtifactReadUnavailable
}

type catalogArtifactExecutor struct{}

func (catalogArtifactExecutor) Execute(_ context.Context, v tool.ReadArtifactInvocation) tool.ReadArtifactExecution {
	p, _ := tool.NewModelPreview("ReadArtifact failed: unavailable")
	r, _ := tool.NewReadArtifactInvocationResult(v, tool.ResultError, "unavailable", p, tool.ArtifactPage{})
	e, _ := tool.NewReadArtifactFailure(r)
	return e
}

package openai

import (
	"context"
	"easycode/internal/tool"
)

type catalogArtifactExecutor struct{}

func (catalogArtifactExecutor) Execute(_ context.Context, v tool.ReadArtifactInvocation) tool.ReadArtifactExecution {
	p, _ := tool.NewModelPreview("ReadArtifact failed: unavailable")
	r, _ := tool.NewReadArtifactInvocationResult(v, tool.ResultError, "unavailable", p, tool.ArtifactPage{})
	execution, _ := tool.NewReadArtifactFailure(r)
	return execution
}

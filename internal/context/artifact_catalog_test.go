package context

import (
	"context"
	"easycode/internal/tool"
)

type catalogArtifactExecutor struct{}

func (catalogArtifactExecutor) Execute(_ context.Context, v tool.ReadArtifactInvocation) tool.InvocationResult {
	p, _ := tool.NewModelPreview("ReadArtifact failed: unavailable")
	r, _ := tool.NewReadArtifactInvocationResult(v, tool.ResultError, "unavailable", p, tool.ArtifactPage{})
	return r
}

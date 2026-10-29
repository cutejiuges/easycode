package tool

import (
	"context"
)

type catalogArtifactExecutor struct{}

func (catalogArtifactExecutor) Execute(_ context.Context, v ReadArtifactInvocation) InvocationResult {
	p, _ := NewModelPreview("ReadArtifact failed: unavailable")
	r, _ := NewReadArtifactInvocationResult(v, ResultError, "unavailable", p, ArtifactPage{})
	return r
}

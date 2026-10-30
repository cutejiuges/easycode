package tool

import (
	"context"
)

type catalogArtifactExecutor struct{}

func (catalogArtifactExecutor) Execute(_ context.Context, v ReadArtifactInvocation) ReadArtifactExecution {
	p, _ := NewModelPreview("ReadArtifact failed: unavailable")
	r, _ := NewReadArtifactInvocationResult(v, ResultError, "unavailable", p, ArtifactPage{})
	execution, _ := NewReadArtifactFailure(r)
	return execution
}

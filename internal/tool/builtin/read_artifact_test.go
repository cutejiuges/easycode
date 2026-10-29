package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/tool"
)

type artifactFailureReader struct {
	err   error
	calls int
}

func (r *artifactFailureReader) ReadArtifact(context.Context, tool.ReadArtifactInput) (tool.ArtifactPage, error) {
	r.calls++
	return tool.ArtifactPage{}, r.err
}

func TestReadArtifactErrorsAreSafeAndCancellationAvoidsIO(t *testing.T) {
	input, _ := tool.NewReadArtifactInput("0123456789abcdef0123456789abcdef", 0, 3)
	call, _ := tool.NewReadArtifactReadyCall("read", input)
	id, _ := tool.GenerateInvocationID()
	v, _ := tool.NewReadArtifactInvocation(id, call)
	for _, test := range []struct {
		err  error
		code string
	}{
		{errors.New("/private/secret: confidential bytes"), "unavailable"},
		{tool.ArtifactReadUnauthorized, "unauthorized"}, {tool.ArtifactReadCorrupt, "corrupt"}, {tool.ArtifactReadInvalidRange, "invalid_range"},
	} {
		reader := &artifactFailureReader{err: test.err}
		executor, _ := NewReadArtifactExecutor(reader)
		r := executor.Execute(context.Background(), v)
		if r.Validate() != nil || r.Code() != test.code || strings.Contains(r.Preview().Text(), "secret") || strings.Contains(r.Preview().Text(), "confidential") {
			t.Fatal("unsafe artifact error")
		}
		if _, ok := r.ArtifactPage(); ok {
			t.Fatal("failed read exposed page")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		before := reader.calls
		if r := executor.Execute(ctx, v); r.Status() != tool.ResultCancelled || reader.calls != before {
			t.Fatal("pre-cancelled read accessed storage")
		}
	}
}

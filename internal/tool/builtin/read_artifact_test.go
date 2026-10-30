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

func (r *artifactFailureReader) ReadArtifact(context.Context, tool.ReadArtifactInput) (tool.ArtifactWindow, error) {
	r.calls++
	return tool.ArtifactWindow{}, r.err
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
		r, err := executor.Execute(context.Background(), v).Finalize(tool.MaxModelPreviewBytes)
		if err != nil {
			t.Fatal(err)
		}
		if r.Validate() != nil || r.Code() != test.code || strings.Contains(r.Preview().Text(), "secret") || strings.Contains(r.Preview().Text(), "confidential") {
			t.Fatal("unsafe artifact error")
		}
		if _, ok := r.ArtifactPage(); ok {
			t.Fatal("failed read exposed page")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		before := reader.calls
		if r, err := executor.Execute(ctx, v).Finalize(tool.MaxModelPreviewBytes); err != nil || r.Status() != tool.ResultCancelled || reader.calls != before {
			t.Fatal("pre-cancelled read accessed storage")
		}
	}
}

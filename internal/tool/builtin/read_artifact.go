package builtin

import (
	"context"
	"easycode/internal/tool"
	"errors"
	"fmt"
)

// ReadArtifactExecutor 组合已提交引用授权与范围存储接口。
type ReadArtifactExecutor struct{ reader tool.ArtifactRangeReader }

// NewReadArtifactExecutor 纯内存装配真实读取边界。
func NewReadArtifactExecutor(reader tool.ArtifactRangeReader) (*ReadArtifactExecutor, error) {
	if reader == nil {
		return nil, errors.New("artifact reader is required")
	}
	return &ReadArtifactExecutor{reader}, nil
}

// Execute 在完整 ready 后读取；错误不传播底层路径或正文。
func (e *ReadArtifactExecutor) Execute(ctx context.Context, v tool.ReadArtifactInvocation) tool.ReadArtifactExecution {
	var window tool.ArtifactWindow
	var err error
	if ctx == nil {
		err = context.Canceled
	} else {
		err = ctx.Err()
	}
	if err == nil {
		window, err = e.reader.ReadArtifact(ctx, v.Input())
	}
	if err == nil {
		execution, executionErr := tool.NewReadArtifactExecution(v, window)
		if executionErr == nil {
			return execution
		}
		err = tool.ArtifactReadCorrupt
	}
	status, code := tool.ResultError, "unavailable"
	for _, failure := range []tool.ArtifactReadFailure{tool.ArtifactReadUnauthorized, tool.ArtifactReadCorrupt, tool.ArtifactReadInvalidRange} {
		if errors.Is(err, failure) {
			code = string(failure)
			break
		}
	}
	if ctx == nil || ctx.Err() != nil {
		status, code = tool.ResultCancelled, "cancelled"
	}
	message := "ReadArtifact failed: " + code
	var rangeErr tool.ArtifactRangeError
	if errors.As(err, &rangeErr) {
		message += fmt.Sprintf("; minimum_limit: %d", rangeErr.MinimumLimit())
	}
	preview, _ := tool.NewModelPreview(message)
	result, _ := tool.NewReadArtifactInvocationResult(v, status, code, preview, tool.ArtifactPage{})
	execution, _ := tool.NewReadArtifactFailure(result)
	return execution
}

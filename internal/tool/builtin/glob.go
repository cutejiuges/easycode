package builtin

import (
	"context"
	"errors"

	"easycode/internal/tool"
)

// GlobExecutor 只依赖已打开 Workspace capability 的 Glob 执行器。
type GlobExecutor struct{ workspace *Workspace }

// NewGlobExecutor 纯内存组合一个 Glob executor，不访问文件系统。
func NewGlobExecutor(workspace *Workspace) (*GlobExecutor, error) {
	if workspace == nil || workspace.handle == nil {
		return nil, errors.New("workspace is required")
	}
	return &GlobExecutor{workspace: workspace}, nil
}

// Execute 在安全 walker 结果上执行确定性 Glob 匹配。
func (executor *GlobExecutor) Execute(ctx context.Context, invocation tool.GlobInvocation) tool.InvocationResult {
	if ctx == nil || ctx.Err() != nil {
		return globError(invocation, tool.ResultCancelled, "cancelled", "Glob cancelled")
	}
	if executor == nil || executor.workspace == nil || invocation.Validate() != nil {
		return globError(invocation, tool.ResultError, "invalid_invocation", "Glob failed: invalid invocation")
	}
	matcher, err := compileGlobMatcher(invocation.Input().Pattern())
	if err != nil {
		return globError(invocation, tool.ResultError, "invalid_pattern", "Glob failed: invalid pattern")
	}
	walked, err := walkWorkspace(ctx, executor.workspace, invocation.Input().Path())
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return globError(invocation, tool.ResultCancelled, "cancelled", "Glob cancelled")
		}
		return globError(invocation, tool.ResultError, "search_unavailable", "Glob failed: workspace is unavailable")
	}
	matches := make([]string, 0)
	for _, file := range walked.files {
		if matcher.Match(file.relativePath) {
			matches = append(matches, file.relativePath)
		}
	}
	omitted := 0
	if len(matches) > invocation.Input().Limit() {
		omitted = len(matches) - invocation.Input().Limit()
		matches = matches[:invocation.Input().Limit()]
	}
	preview, previewOmitted := renderSearchPreview(matches)
	omitted += previewOmitted
	truncated := omitted > 0 || walked.incompleteReason != tool.SearchComplete
	metadata, err := tool.NewGlobResultMetadata(matches, truncated, omitted, walked.visitedEntries, walked.incompleteReason, walked.skipped.freeze())
	if err != nil {
		return globError(invocation, tool.ResultError, "internal_error", "Glob failed: internal error")
	}
	result, err := tool.NewGlobInvocationResult(invocation, tool.ResultSuccess, "ok", preview, metadata)
	if err != nil {
		return globError(invocation, tool.ResultError, "internal_error", "Glob failed: internal error")
	}
	return result
}

func globError(invocation tool.GlobInvocation, status tool.ResultStatus, code string, message string) tool.InvocationResult {
	preview, _ := tool.NewModelPreview(message)
	metadata, _ := tool.NewGlobResultMetadata([]string{}, false, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	result, err := tool.NewGlobInvocationResult(invocation, status, code, preview, metadata)
	if err == nil {
		return result
	}
	return tool.InvocationResult{}
}

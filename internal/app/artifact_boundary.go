package app

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"

	"easycode/internal/fault"
	"easycode/internal/session/artifact"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
)

// commandArtifactBoundary 在每次执行前验证当前存储布局，随后委派已有执行 owner。
type commandArtifactBoundary struct {
	layout   *artifact.Layout
	executor tool.ExecExecutor
}

// artifactOutputStore 将存储生命周期适配为 executor 所需的小接口。
type artifactOutputStore struct{ store *artifact.Store }

func (s artifactOutputStore) StartCapture(ctx context.Context, id tool.InvocationID) (tool.CommandOutputCapture, error) {
	c, err := s.store.Start(ctx, id)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// artifactRangeReader 在任何磁盘操作前解析当前线程已提交引用。
type artifactRangeReader struct {
	store      *artifact.Store
	references *tool.ArtifactReferences
}

func (r artifactRangeReader) ReadArtifact(ctx context.Context, input tool.ReadArtifactInput) (tool.ArtifactWindow, error) {
	d, ok := r.references.Lookup(input.ID())
	if !ok {
		return tool.ArtifactWindow{}, tool.ArtifactReadUnauthorized
	}
	return r.store.ReadWindow(ctx, d, input)
}

func openCommandArtifactBoundary(ctx context.Context, root, workspace string) (*artifact.Layout, error) {
	// 仅规范化 macOS 的系统级别名，私有路径中的任意 symlink 仍交给 Layout 拒绝。
	canonical := filepath.Clean(root)
	if runtime.GOOS == "darwin" {
		for _, alias := range []string{"/var", "/tmp", "/etc"} {
			if canonical == alias || strings.HasPrefix(canonical, alias+"/") {
				resolved, err := filepath.EvalSymlinks(alias)
				if err != nil {
					return nil, fault.New(fault.CodeInvalidConfiguration, "unsafe_storage_layout")
				}
				canonical = resolved + strings.TrimPrefix(canonical, alias)
				break
			}
		}
	}
	l, err := artifact.OpenLayout(ctx, canonical, workspace, builtin.CommandReadRoots(workspace), []string{workspace})
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "unsafe_storage_layout")
	}
	return l, nil
}

func (b *commandArtifactBoundary) Execute(ctx context.Context, candidate tool.ExecCandidate, progress func(tool.ExecProgress)) tool.InvocationResult {
	if err := b.layout.Check(ctx); err != nil {
		return tool.NewExecErrorResult(candidate.Invocation(), tool.ResultError, "unsafe_storage_layout", "Exec failed: unsafe_storage_layout")
	}
	return b.executor.Execute(ctx, candidate, progress)
}

// StartTextCapture 为搜索调用适配同一私有存储与宿主生命周期。
func (s artifactOutputStore) StartTextCapture(ctx context.Context, id tool.InvocationID, capability tool.CapabilityID) (tool.TextOutputCapture, error) {
	capture, err := s.store.StartText(ctx, id, capability)
	if err != nil {
		return nil, err
	}
	return capture, nil
}

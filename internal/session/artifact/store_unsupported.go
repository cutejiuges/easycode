//go:build !darwin && !linux

package artifact

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// Store 在缺少描述符绑定文件操作的平台拒绝获取资源。
type Store struct{}

// Open 不允许以普通路径操作替代私有存储边界。
func Open(context.Context, *Layout, domain.ThreadID) (*Store, error) {
	return nil, tool.ArtifactReadUnavailable
}

// Start 在未支持的平台不创建 staging 或预留。
func (*Store) Start(context.Context, tool.InvocationID) (tool.CommandOutputCapture, error) {
	return nil, tool.ArtifactReadUnavailable
}

// Read 在未支持的平台不读取外部文件。
func (*Store) Read(context.Context, tool.ArtifactDescriptor, tool.ReadArtifactInput) (tool.ArtifactPage, error) {
	return tool.ArtifactPage{}, tool.ArtifactReadUnavailable
}

// Clean 在未支持的平台不执行删除。
func (*Store) Clean(context.Context, []tool.ArtifactDescriptor) error {
	return tool.ArtifactReadUnavailable
}

// Close 没有需要释放的资源。
func (*Store) Close() error { return nil }

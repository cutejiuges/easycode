package artifact

import (
	"context"

	"easycode/internal/tool"
)

// CleanupReport 记录孤儿回收是否改变目录，以及是否已完成同步。
type CleanupReport struct {
	Changed bool
	Durable bool
}

// Clean 沿用只需观察错误的孤儿回收入口。
func (s *Store) Clean(ctx context.Context, referenced []tool.ArtifactDescriptor) error {
	_, err := s.CleanWithReport(ctx, referenced)
	return err
}

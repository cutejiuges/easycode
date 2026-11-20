package builtin

import (
	"context"

	"easycode/internal/executor/process"
	"easycode/internal/tool"
)

// execOwnerLauncher 将当前候选绑定到共用生命周期，不改变普通工具准入。
type execOwnerLauncher struct {
	launcher  processLauncher
	candidate tool.ExecCandidate
}

func (l execOwnerLauncher) Start(ctx context.Context) (process.Handle, error) {
	return l.launcher.start(ctx, l.candidate)
}

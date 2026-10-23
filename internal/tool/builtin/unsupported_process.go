package builtin

import (
	"context"

	"easycode/internal/tool"
)

// unsupportedProcessLauncher 让不具备等价隔离的平台失败关闭；所有平台均可验证这一执行边界。
type unsupportedProcessLauncher struct{}

func (unsupportedProcessLauncher) start(context.Context, tool.ExecCandidate) (processHandle, error) {
	return nil, errExecUnsupported
}

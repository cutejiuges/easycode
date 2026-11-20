//go:build !darwin

package builtin

import (
	"context"

	"easycode/internal/executor/process"
	"easycode/internal/tool"
)

// unsupportedProcessLauncher 让不具备等价隔离的平台失败关闭。
type unsupportedProcessLauncher struct{}

func (unsupportedProcessLauncher) start(context.Context, tool.ExecCandidate) (process.Handle, error) {
	return nil, process.Unsupported
}

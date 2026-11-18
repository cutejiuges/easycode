//go:build darwin

package builtin

import (
	"context"
	"easycode/internal/tool"
)

// testCommandOutputStore 在既有执行器测试中显式模拟存储不可用。
type testCommandOutputStore struct{}

func (testCommandOutputStore) StartCapture(context.Context, tool.InvocationID) (tool.CommandOutputCapture, error) {
	return nil, tool.ArtifactReadUnavailable
}

// Package builtin 组合当前唯一可执行的内置 Read 能力。
package builtin

import "easycode/internal/tool"

// NewCatalog 创建只暴露 Read 的确定性目录快照。
func NewCatalog(executor tool.ReadExecutor) (tool.CatalogSnapshot, error) {
	return tool.NewReadCatalogSnapshot(executor)
}

// Package builtin 组合当前可执行的内置只读能力。
package builtin

import "easycode/internal/tool"

// NewCatalog 创建 Read、Glob、Grep 均已绑定真实执行器的确定性目录快照。
func NewCatalog(readExecutor tool.ReadExecutor, globExecutor tool.GlobExecutor, grepExecutor tool.GrepExecutor) (tool.CatalogSnapshot, error) {
	return tool.NewReadOnlyCatalogSnapshot(readExecutor, globExecutor, grepExecutor)
}

// Package builtin 组合当前可执行的内置能力。
package builtin

import "easycode/internal/tool"

// NewCatalog 创建五种能力均已绑定真实组件的确定性目录快照。
func NewCatalog(
	readExecutor tool.ReadExecutor,
	globExecutor tool.GlobExecutor,
	grepExecutor tool.GrepExecutor,
	patchPreparer tool.PatchPreparer,
	patchExecutor tool.PatchExecutor,
	writePreparer tool.WritePreparer,
	writeExecutor tool.WriteExecutor,
	policy tool.Policy,
) (tool.CatalogSnapshot, error) {
	return tool.NewCatalogSnapshot(
		readExecutor, globExecutor, grepExecutor, patchPreparer, patchExecutor, writePreparer, writeExecutor, policy,
	)
}

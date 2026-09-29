// Package plugin 定义本地 Plugin manifest 和导入边界。
package plugin

import (
	"context"

	"easycode/internal/extension"
)

// TODO(P6): Plugin manifest 导入边界仅为扩展系统阶段保留，当前不得扫描或加载插件；由后续 OpenSpec 同时提供真实消费者、验证与测试时启用，否则删除。

// Manifest 保存 EasyCode 原生 Plugin 的最小元数据。
type Manifest struct {
	Name        string
	Version     string
	Description string
	Root        string
}

// Importer 将原生、Claude 或 Codex manifest 转换为内部贡献。
type Importer interface {
	Import(context.Context, Manifest) (extension.Contribution, error)
}

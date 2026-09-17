// Package plugin 定义本地 Plugin manifest 和导入边界。
package plugin

import (
	"context"

	"easycode/internal/extension"
)

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

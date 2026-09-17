// Package skill 定义渐进披露 Skill 的发现和读取边界。
package skill

import "context"

// Authority 标识 Skill 资源所属的读取权限域。
type Authority struct {
	Kind string
	ID   string
}

// Metadata 是稳定注入模型上下文的轻量 Skill 目录项。
type Metadata struct {
	Name        string
	Description string
	Locator     string
	Authority   Authority
}

// Document 是按需加载的完整 SKILL.md 内容。
type Document struct {
	Metadata Metadata
	Content  string
}

// Source 只能读取自己声明 authority 下的 Skill 资源。
type Source interface {
	List(context.Context) ([]Metadata, error)
	Read(context.Context, Metadata) (Document, error)
}

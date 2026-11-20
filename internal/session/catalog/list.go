package catalog

import (
	"context"
	"fmt"
	"path/filepath"

	"easycode/internal/domain"
)

// ListScope 明确区分单个创建目录与全部目录。
type ListScope uint8

const (
	ListCurrentDirectory ListScope = iota + 1
	ListAllDirectories
	DefaultListLimit = 20
	MaximumListLimit = 200
)

// ListQuery 描述与模型配置无关的有界发现条件。
type ListQuery struct {
	Scope           ListScope
	CreationCWD     string
	ExcludeThreadID domain.ThreadID
	Limit           int
}

// Validate 在访问存储前验证范围、规范化目录、排除身份和数量。
func (query ListQuery) Validate() error {
	if query.Limit < 1 || query.Limit > MaximumListLimit {
		return fmt.Errorf("session list limit must be between 1 and 200")
	}
	if query.ExcludeThreadID != "" && !query.ExcludeThreadID.Valid() {
		return fmt.Errorf("excluded session thread ID is invalid")
	}
	switch query.Scope {
	case ListCurrentDirectory:
		if !filepath.IsAbs(query.CreationCWD) || filepath.Clean(query.CreationCWD) != query.CreationCWD {
			return fmt.Errorf("session list directory must be normalized and absolute")
		}
	case ListAllDirectories:
		if query.CreationCWD != "" {
			return fmt.Errorf("all-directory query must not specify a directory")
		}
	default:
		return fmt.Errorf("session list scope is invalid")
	}
	return nil
}

// ListResult 只返回已索引元数据和同一范围内的剩余条目标记。
type ListResult struct {
	Entries []Entry
	HasMore bool
}

// List 查询已协调的目录，不加载 journal 或执行恢复操作。
func (catalog *Catalog) List(ctx context.Context, query ListQuery) (ListResult, error) {
	if err := query.Validate(); err != nil {
		return ListResult{}, err
	}
	if err := catalog.ready(ctx); err != nil {
		return ListResult{}, err
	}
	var result ListResult
	for _, entry := range catalog.entries {
		if err := ctx.Err(); err != nil {
			return ListResult{}, err
		}
		if entry.ThreadID == query.ExcludeThreadID ||
			(query.Scope == ListCurrentDirectory && entry.CreationCWD != query.CreationCWD) {
			continue
		}
		if len(result.Entries) == query.Limit {
			result.HasMore = true
			break
		}
		result.Entries = append(result.Entries, entry)
	}
	if err := ctx.Err(); err != nil {
		return ListResult{}, err
	}
	return result, nil
}

package catalog

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	Scope       ListScope
	CreationCWD string
	Limit       int
}

// Validate 在访问存储前验证范围、规范化目录和数量。
func (query ListQuery) Validate() error {
	if query.Limit < 1 || query.Limit > MaximumListLimit {
		return fmt.Errorf("session list limit must be between 1 and 200")
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
func (catalog *Catalog) List(ctx context.Context, query ListQuery) (result ListResult, resultErr error) {
	if err := query.Validate(); err != nil {
		return ListResult{}, err
	}
	if err := catalog.ready(ctx); err != nil {
		return ListResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ListResult{}, err
	}
	sql := `SELECT session_id, thread_id, journal_path, created_at_ns, updated_at_ns,
last_sequence, last_checksum, creation_cwd, provider_family, provider_wire, model
FROM threads`
	if query.Scope == ListCurrentDirectory {
		sql += ` WHERE creation_cwd=?2`
	}
	sql += ` ORDER BY updated_at_ns DESC, thread_id DESC LIMIT ?1`
	statement, _, err := catalog.db.Prepare(sql)
	if err != nil {
		return ListResult{}, fmt.Errorf("prepare session list: %w", err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, statement.Close())
		if resultErr != nil {
			result = ListResult{}
		}
	}()
	if err := statement.BindInt64(1, int64(query.Limit+1)); err != nil {
		return ListResult{}, err
	}
	if query.Scope == ListCurrentDirectory {
		if err := statement.BindText(2, query.CreationCWD); err != nil {
			return ListResult{}, err
		}
	}
	for statement.Step() {
		if err := ctx.Err(); err != nil {
			return ListResult{}, err
		}
		entry, err := decodeEntry(statement)
		if err != nil {
			return ListResult{}, err
		}
		if len(result.Entries) == query.Limit {
			result.HasMore = true
			break
		}
		result.Entries = append(result.Entries, entry)
	}
	if err := statement.Err(); err != nil {
		return ListResult{}, fmt.Errorf("query session list: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return ListResult{}, err
	}
	return result, nil
}

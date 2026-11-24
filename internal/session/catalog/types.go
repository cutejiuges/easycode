// Package catalog 实现从 Session JSONL 事实源派生的可重建会话目录。
package catalog

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// Config 描述 Catalog 的显式存储位置和协调边界。
type Config struct {
	SnapshotPath string
}

// Entry 是一个已完整回放的可恢复线程的最小查询投影。
type Entry struct {
	SessionID      domain.SessionID      `json:"session_id"`
	ThreadID       domain.ThreadID       `json:"thread_id"`
	ParentThreadID parentThreadID        `json:"parent_thread_id,omitempty"`
	JournalPath    string                `json:"journal_path"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
	LastSequence   uint64                `json:"last_sequence"`
	LastChecksum   string                `json:"last_checksum"`
	CreationCWD    string                `json:"creation_cwd"`
	ProviderFamily domain.ProviderFamily `json:"provider_family"`
	ProviderWire   string                `json:"provider_wire"`
	Model          string                `json:"model"`
	Title          domain.SessionTitle   `json:"title"`
}

// parentThreadID 只在分支存在。解码时区分缺省与显式空值。
type parentThreadID domain.ThreadID

func (id *parentThreadID) UnmarshalJSON(data []byte) error {
	var raw string
	if err := codec.UnmarshalStrict(data, &raw); err != nil {
		return err
	}
	parsed, err := domain.ParseThreadID(raw)
	if err != nil {
		return fmt.Errorf("catalog parent thread ID is invalid")
	}
	*id = parentThreadID(parsed)
	return nil
}

// Selector 描述 --continue 的精确兼容条件。
type Selector struct {
	CreationCWD    string
	ProviderFamily domain.ProviderFamily
	ProviderWire   string
	Model          string
}

// ReconciliationReport 汇总一次完整前台协调的分类结果。
type ReconciliationReport struct {
	Present       int
	Valid         int
	Busy          int
	Invalid       int
	BusyUnindexed bool
}

func validateConfig(config Config) error {
	if !filepath.IsAbs(config.SnapshotPath) || filepath.Base(filepath.Clean(config.SnapshotPath)) == string(filepath.Separator) {
		return fmt.Errorf("catalog snapshot path must be absolute")
	}
	return nil
}

func validateEntry(entry Entry) error {
	if !entry.SessionID.Valid() || !entry.ThreadID.Valid() {
		return fmt.Errorf("catalog entry identity is invalid")
	}
	if entry.ParentThreadID != "" && (!domain.ThreadID(entry.ParentThreadID).Valid() || domain.ThreadID(entry.ParentThreadID) == entry.ThreadID || entry.Title == "") {
		return fmt.Errorf("catalog branch metadata is invalid")
	}
	if !validRelativeJournalPath(entry.JournalPath, entry.ThreadID) {
		return fmt.Errorf("catalog entry journal path is invalid")
	}
	if entry.CreatedAt.IsZero() || entry.CreatedAt.Location() != time.UTC ||
		entry.UpdatedAt.IsZero() || entry.UpdatedAt.Location() != time.UTC {
		return fmt.Errorf("catalog entry timestamp is invalid")
	}
	if entry.LastSequence == 0 || strings.TrimSpace(entry.LastChecksum) == "" {
		return fmt.Errorf("catalog entry tail is invalid")
	}
	if !filepath.IsAbs(entry.CreationCWD) || filepath.Clean(entry.CreationCWD) != entry.CreationCWD {
		return fmt.Errorf("catalog entry creation cwd is invalid")
	}
	if !entry.ProviderFamily.Valid() || strings.TrimSpace(entry.ProviderWire) == "" || strings.TrimSpace(entry.Model) == "" {
		return fmt.Errorf("catalog entry provider metadata is invalid")
	}
	if entry.Title != "" && entry.Title.Validate() != nil {
		return fmt.Errorf("catalog entry title is invalid")
	}
	return nil
}

func validateSelector(selector Selector) error {
	if !filepath.IsAbs(selector.CreationCWD) || filepath.Clean(selector.CreationCWD) != selector.CreationCWD {
		return fmt.Errorf("catalog selector creation cwd is invalid")
	}
	if !selector.ProviderFamily.Valid() || strings.TrimSpace(selector.ProviderWire) == "" || strings.TrimSpace(selector.Model) == "" {
		return fmt.Errorf("catalog selector provider metadata is invalid")
	}
	return nil
}

func validRelativeJournalPath(relative string, threadID domain.ThreadID) bool {
	if relative == "" || strings.Contains(relative, `\`) || path.IsAbs(relative) || path.Clean(relative) != relative {
		return false
	}
	timestamp, err := threadID.Time()
	if err != nil {
		return false
	}
	want := path.Join(timestamp.Format("2006"), timestamp.Format("01"), timestamp.Format("02"), string(threadID)+".jsonl")
	return relative == want
}

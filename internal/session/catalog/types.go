// Package catalog 实现从 Session JSONL 事实源派生的可重建 SQLite 目录。
package catalog

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"easycode/internal/domain"
)

// Config 描述 Catalog 的显式存储位置和协调边界。
type Config struct {
	DatabasePath string
	SessionRoot  string
	BusyTimeout  time.Duration
}

// Entry 是一个已完整回放的 root thread 的最小查询投影。
type Entry struct {
	SessionID      domain.SessionID
	ThreadID       domain.ThreadID
	JournalPath    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastSequence   uint64
	LastChecksum   string
	CreationCWD    string
	ProviderFamily domain.ProviderFamily
	ProviderWire   string
	Model          string
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
	if config.DatabasePath == "" || !filepath.IsAbs(config.DatabasePath) {
		return fmt.Errorf("catalog database path must be absolute")
	}
	if config.SessionRoot == "" || !filepath.IsAbs(config.SessionRoot) {
		return fmt.Errorf("catalog session root must be absolute")
	}
	if config.BusyTimeout < 0 {
		return fmt.Errorf("catalog busy timeout is invalid")
	}
	return nil
}

func validateEntry(entry Entry) error {
	if !entry.SessionID.Valid() || !entry.ThreadID.Valid() {
		return fmt.Errorf("catalog entry identity is invalid")
	}
	if !validRelativeJournalPath(entry.JournalPath, entry.ThreadID) {
		return fmt.Errorf("catalog entry journal path is invalid")
	}
	if entry.CreatedAt.IsZero() || entry.CreatedAt.Location() != time.UTC ||
		entry.UpdatedAt.IsZero() || entry.UpdatedAt.Location() != time.UTC || entry.UpdatedAt.Before(entry.CreatedAt) {
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

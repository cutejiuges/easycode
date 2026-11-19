//go:build darwin || linux

package artifact

import (
	"context"
	"errors"
	"os"
	"strings"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

// CleanWithReport 在调用方持有线程 lease 且已完成 journal 校验后回收当前线程孤儿。
// 引用集合必须来自完整 replay；本函数不会读取其他线程的 journal 或删除其内容。
func (s *Store) CleanWithReport(ctx context.Context, referenced []tool.ArtifactDescriptor) (CleanupReport, error) {
	var report CleanupReport
	if ctx == nil || s == nil || s.root == nil {
		return report, tool.ArtifactReadUnavailable
	}
	keep := make(map[string]map[string]bool)
	for _, d := range referenced {
		if d.Validate() != nil || d.ThreadID() != s.threadID {
			return report, tool.ArtifactReadUnauthorized
		}
		key := string(d.InvocationID())
		if keep[key] == nil {
			keep[key] = make(map[string]bool)
		}
		keep[key][string(d.ID())] = true
	}
	guard, err := lockAccounting(ctx, s.root)
	if err != nil {
		return report, err
	}
	defer func() { _ = guard.Close() }()
	entries, err := names(s.thread)
	if err != nil {
		return report, tool.ArtifactReadUnavailable
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		if _, err = tool.ParseInvocationID(entry); err != nil {
			return report, tool.ArtifactReadCorrupt
		}
		dir, err := openChildDirectory(s.thread, entry, false)
		if err != nil {
			return report, tool.ArtifactReadUnavailable
		}
		changed, err := cleanInvocation(dir, keep[entry])
		report.Changed = report.Changed || changed
		_ = dir.Close()
		if err != nil {
			return report, err
		}
		if len(keep[entry]) == 0 {
			if err = unix.Unlinkat(int(s.thread.Fd()), entry, unix.AT_REMOVEDIR); err != nil {
				return report, tool.ArtifactReadUnavailable
			}
			report.Changed = true
		}
	}
	if err = s.thread.Sync(); err != nil {
		return report, tool.ArtifactReadUnavailable
	}
	report.Durable = report.Changed
	return report, nil
}

func cleanInvocation(dir *os.File, keep map[string]bool) (bool, error) {
	entries, err := names(dir)
	if err != nil {
		return false, tool.ArtifactReadUnavailable
	}
	var reservation *os.File
	changed := false
	for _, name := range entries {
		if name != "reservation" {
			if _, err = tool.ParseArtifactID(strings.TrimSuffix(name, ".stage")); err != nil {
				return false, tool.ArtifactReadCorrupt
			}
		}
		f, err := openObject(dir, name, unix.O_RDONLY)
		if err != nil {
			return false, tool.ArtifactReadUnavailable
		}
		if name == "reservation" {
			reservation = f
			defer func() { _ = f.Close() }()
		} else {
			_ = f.Close()
		}
	}
	if reservation != nil {
		if err = unix.Flock(int(reservation.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			return false, errors.New("artifact capture is active")
		}
	}
	for _, name := range entries {
		if keep[name] || (name == "reservation" && len(keep) > 0) {
			continue
		}
		if err = unix.Unlinkat(int(dir.Fd()), name, 0); err != nil {
			return changed, tool.ArtifactReadUnavailable
		}
		changed = true
	}
	if err = dir.Sync(); err != nil {
		return changed, tool.ArtifactReadUnavailable
	}
	return changed, nil
}

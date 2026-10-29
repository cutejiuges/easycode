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

// Clean 在调用方持有线程 lease 且已完成 journal 校验后回收当前线程孤儿。
// 引用集合必须来自完整 replay；本函数不会读取其他线程的 journal 或删除其内容。
func (s *Store) Clean(ctx context.Context, referenced []tool.ArtifactDescriptor) error {
	if ctx == nil || s == nil || s.root == nil {
		return tool.ArtifactReadUnavailable
	}
	keep := make(map[string]map[string]bool)
	for _, d := range referenced {
		if d.Validate() != nil || d.ThreadID() != s.threadID {
			return tool.ArtifactReadUnauthorized
		}
		key := string(d.InvocationID())
		if keep[key] == nil {
			keep[key] = make(map[string]bool)
		}
		keep[key][string(d.ID())] = true
	}
	guard, err := lockAccounting(ctx, s.root)
	if err != nil {
		return err
	}
	defer func() { _ = guard.Close() }()
	entries, err := names(s.thread)
	if err != nil {
		return tool.ArtifactReadUnavailable
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if _, err = tool.ParseInvocationID(entry); err != nil {
			return tool.ArtifactReadCorrupt
		}
		dir, err := openChildDirectory(s.thread, entry, false)
		if err != nil {
			return tool.ArtifactReadUnavailable
		}
		err = cleanInvocation(dir, keep[entry])
		_ = dir.Close()
		if err != nil {
			return err
		}
		if len(keep[entry]) == 0 {
			if err = unix.Unlinkat(int(s.thread.Fd()), entry, unix.AT_REMOVEDIR); err != nil {
				return tool.ArtifactReadUnavailable
			}
		}
	}
	if err = s.thread.Sync(); err != nil {
		return tool.ArtifactReadUnavailable
	}
	return nil
}

func cleanInvocation(dir *os.File, keep map[string]bool) error {
	entries, err := names(dir)
	if err != nil {
		return tool.ArtifactReadUnavailable
	}
	var reservation *os.File
	for _, name := range entries {
		if name != "reservation" {
			if _, err = tool.ParseArtifactID(strings.TrimSuffix(name, ".stage")); err != nil {
				return tool.ArtifactReadCorrupt
			}
		}
		f, err := openObject(dir, name, unix.O_RDONLY)
		if err != nil {
			return tool.ArtifactReadUnavailable
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
			return errors.New("artifact capture is active")
		}
	}
	for _, name := range entries {
		if keep[name] || (name == "reservation" && len(keep) > 0) {
			continue
		}
		if err = unix.Unlinkat(int(dir.Fd()), name, 0); err != nil {
			return tool.ArtifactReadUnavailable
		}
	}
	if err = dir.Sync(); err != nil {
		return tool.ArtifactReadUnavailable
	}
	return nil
}

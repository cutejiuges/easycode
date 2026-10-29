//go:build darwin || linux

// Package artifact 提供命令输出的私有存储与范围读取边界。
package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var errUnsafeLayout = errors.New("unsafe_storage_layout")

type directoryIdentity struct {
	device uint64
	inode  uint64
}

func identity(file *os.File) (directoryIdentity, error) {
	var st unix.Stat_t
	if file == nil || unix.Fstat(int(file.Fd()), &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return directoryIdentity{}, errUnsafeLayout
	}
	return directoryIdentity{uint64(st.Dev), uint64(st.Ino)}, nil
}

// Layout 持有隔离边界的目录身份；只有 Close 可以释放这些句柄。
type Layout struct {
	storage, workspace         *os.File
	storagePath, workspacePath string
	readRoots, writeRoots      []string
}

// OpenLayout 验证已存在的存储根与实际命令权限；不创建目录。
func OpenLayout(ctx context.Context, storage, workspace string, readRoots, writeRoots []string) (*Layout, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, errUnsafeLayout
	}
	s, err := openDirectoryPath(storage, false)
	if err != nil {
		return nil, errUnsafeLayout
	}
	w, err := openDirectoryPath(workspace, true)
	if err != nil {
		_ = s.Close()
		return nil, errUnsafeLayout
	}
	l := &Layout{storage: s, workspace: w, storagePath: storage, workspacePath: workspace, readRoots: append([]string(nil), readRoots...), writeRoots: append([]string(nil), writeRoots...)}
	if err = l.Check(ctx); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

// Check 重开路径并核对身份、实际祖先关系；不使用字符串前缀。
func (l *Layout) Check(ctx context.Context) error {
	if ctx == nil || ctx.Err() != nil || l == nil || l.storage == nil || l.workspace == nil {
		return errUnsafeLayout
	}
	s, err := openDirectoryPath(l.storagePath, false)
	if err != nil {
		return errUnsafeLayout
	}
	defer func() { _ = s.Close() }()
	w, err := openDirectoryPath(l.workspacePath, true)
	if err != nil {
		return errUnsafeLayout
	}
	defer func() { _ = w.Close() }()
	if !sameDirectory(s, l.storage) || !sameDirectory(w, l.workspace) {
		return errUnsafeLayout
	}
	var stat unix.Stat_t
	if unix.Fstat(int(s.Fd()), &stat) != nil || stat.Mode&0o777 != 0o700 || stat.Uid != uint32(os.Geteuid()) {
		return errUnsafeLayout
	}
	inside, err := descendsFrom(s, w)
	if err != nil || inside {
		return errUnsafeLayout
	}
	inside, err = descendsFrom(w, s)
	if err != nil || inside {
		return errUnsafeLayout
	}
	roots := append(append([]string(nil), l.readRoots...), l.writeRoots...)
	for _, root := range roots {
		if ctx.Err() != nil {
			return errUnsafeLayout
		}
		r, openErr := openDirectoryPath(root, true)
		if errors.Is(openErr, os.ErrNotExist) {
			continue
		}
		if openErr != nil {
			return errUnsafeLayout
		}
		inside, err = descendsFrom(s, r)
		_ = r.Close()
		if err != nil || inside {
			return errUnsafeLayout
		}
	}
	return nil
}

// Close 释放布局持有的目录，调用方必须先等待使用者退出。
func (l *Layout) Close() error {
	if l == nil {
		return nil
	}
	var result error
	if l.storage != nil {
		result = errors.Join(result, l.storage.Close())
		l.storage = nil
	}
	if l.workspace != nil {
		result = errors.Join(result, l.workspace.Close())
		l.workspace = nil
	}
	return result
}

func sameDirectory(a, b *os.File) bool {
	x, e := identity(a)
	y, f := identity(b)
	return e == nil && f == nil && x == y
}

func descendsFrom(child, ancestor *os.File) (bool, error) {
	want, err := identity(ancestor)
	if err != nil {
		return false, err
	}
	fd, err := unix.Openat(int(child.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	current := os.NewFile(uintptr(fd), "artifact-ancestor")
	defer func() { _ = current.Close() }()
	for depth := 0; depth < 1024; depth++ {
		id, err := identity(current)
		if err != nil {
			return false, err
		}
		if id == want {
			return true, nil
		}
		parentFD, err := unix.Openat(int(current.Fd()), "..", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return false, err
		}
		parent := os.NewFile(uintptr(parentFD), "artifact-parent")
		pid, err := identity(parent)
		if err != nil {
			_ = parent.Close()
			return false, err
		}
		if pid == id {
			_ = parent.Close()
			return false, nil
		}
		_ = current.Close()
		current = parent
	}
	return false, errUnsafeLayout
}

func openDirectoryPath(path string, aliases bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
		return nil, errUnsafeLayout
	}
	// 系统根别名先显式解析，再逐级不跟随打开；后续 Check 校验保留的实际身份。
	if aliases {
		var err error
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), "artifact-root")
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		fd, err = unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		_ = current.Close()
		if err != nil {
			return nil, err
		}
		current = os.NewFile(uintptr(fd), "artifact-directory")
		if _, err = identity(current); err != nil {
			_ = current.Close()
			return nil, err
		}
	}
	return current, nil
}

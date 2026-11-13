//go:build darwin || linux

package localfile

import (
	"context"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// FileIdentity 从实际打开的描述符取得可比较身份。
func FileIdentity(file *os.File) (Identity, error) {
	var stat unix.Stat_t
	if file == nil || unix.Fstat(int(file.Fd()), &stat) != nil {
		return Identity{}, Unsafe
	}
	return Identity{uint64(stat.Dev), uint64(stat.Ino)}, nil
}

// InspectEntry 只分类当前目录项，不将扫描结果当作后续读取的安全授权。
func InspectEntry(root *os.File, name string) Entry {
	entry := Entry{Name: name}
	var stat unix.Stat_t
	if root == nil || !validRelative(name) || strings.Contains(name, "/") || unix.Fstatat(int(root.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil {
		entry.Unsafe = true
		return entry
	}
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		entry.Directory = true
	case unix.S_IFREG:
	default:
		entry.Unsafe = true
	}
	return entry
}

// OpenRelative 从已取得的目录逐段打开，整个过程不重解释绝对路径。
func OpenRelative(ctx context.Context, root *os.File, relative string, directory bool, options Options) (*os.File, error) {
	if ctx == nil || root == nil || !validRelative(relative) || options.RejectWritableFrom != "" {
		return nil, Unsafe
	}
	fd, err := unix.Openat(int(root.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
		if options.BeforeOpen != nil {
			options.BeforeOpen(strings.Join(parts[:i+1], "/"))
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		next, openErr := unix.Openat(fd, part, flags, 0)
		_ = unix.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	file := os.NewFile(uintptr(fd), "local-extension-relative")
	stat, err := file.Stat()
	if err != nil || directory && !stat.IsDir() || !directory && !stat.Mode().IsRegular() {
		_ = file.Close()
		return nil, Unsafe
	}
	return file, nil
}

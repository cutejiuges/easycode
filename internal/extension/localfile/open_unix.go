//go:build darwin || linux

package localfile

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Open 逐段取得实际目录/文件描述符；任何项目组件都不得跟随链接。
func Open(ctx context.Context, p string, directory bool, options Options) (*os.File, error) {
	if ctx == nil || !filepath.IsAbs(p) {
		return nil, Unsafe
	}
	p = SystemPath(filepath.Clean(p))
	protected := SystemPath(filepath.Clean(options.RejectWritableFrom))
	if options.RejectWritableFrom != "" && (!filepath.IsAbs(protected) || p != protected && !strings.HasPrefix(p, protected+"/")) {
		return nil, Unsafe
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			unix.Close(fd)
			return nil, err
		}
		if part == "" {
			continue
		}
		current := filepath.Join("/", filepath.Join(parts[:i+1]...))
		if options.BeforeOpen != nil {
			options.BeforeOpen(current)
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(fd, part, flags, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
		if options.RejectWritableFrom != "" && (current == protected || strings.HasPrefix(current, protected+"/")) {
			var stat unix.Stat_t
			if unix.Fstat(fd, &stat) != nil || stat.Mode&0022 != 0 {
				unix.Close(fd)
				return nil, Unsafe
			}
		}
	}
	return os.NewFile(uintptr(fd), "local-extension"), nil
}

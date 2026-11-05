//go:build darwin || linux

package skill

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
)

// openSecure 从根描述符逐段打开，任何项目组件都不跟随链接。
func openSecure(p string, directory bool, hook func(string)) (*os.File, error) {
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return nil, failure("skill_path_unsafe")
	}
	p = systemSkillPath(p)
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	components := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, c := range components {
		if c == "" {
			continue
		}
		if hook != nil {
			hook(filepath.Join("/", filepath.Join(components[:i+1]...)))
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(components)-1 || directory {
			flags |= unix.O_DIRECTORY
		}
		next, err := unix.Openat(fd, c, flags, 0)
		unix.Close(fd)
		if err != nil {
			return nil, err
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), "skill-resource"), nil
}

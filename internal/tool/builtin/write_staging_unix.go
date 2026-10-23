//go:build darwin || linux

package builtin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type writeOwnedEntry struct {
	parent    *os.File
	file      *os.File
	name      string
	device    uint64
	inode     uint64
	directory bool
}

// writeStagingOwner 只清理自己创建且身份仍匹配的项，不递归处理未知目录内容。
type writeStagingOwner struct {
	entries []writeOwnedEntry
	hooks   writeHooks
	keep    bool
}

func (owner *writeStagingOwner) createRandom(parent *os.File, directory bool) (*os.File, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	return owner.create(parent, ".easycode-write-"+hex.EncodeToString(entropy[:]), directory)
}

func (owner *writeStagingOwner) create(parent *os.File, name string, directory bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW
	mode := uint32(0o600)
	if directory {
		if err := unix.Mkdirat(int(parent.Fd()), name, 0o700); err != nil {
			return nil, err
		}
		flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		mode = 0
	}
	fd, err := unix.Openat(int(parent.Fd()), name, flags, mode)
	if err != nil {
		// mkdir 已成功而未取得句柄时不能确认清理所有权，保留私有项并诚实报告。
		if directory {
			return nil, errors.Join(err, errWriteUncertain)
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "write-staging")
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, errors.Join(err, file.Close(), errWriteUncertain)
	}
	entry := writeOwnedEntry{parent: parent, file: file, name: name, device: uint64(stat.Dev), inode: uint64(stat.Ino), directory: directory}
	owner.entries = append(owner.entries, entry)
	if directory {
		// 显式恢复私有目录基本权限，避免进程 umask 导致后续无法进入。
		if err := file.Chmod(0o700); err != nil {
			return nil, err
		}
	}
	return file, nil
}

func (entry writeOwnedEntry) validateIdentity() error {
	var stat unix.Stat_t
	if err := unix.Fstatat(int(entry.parent.Fd()), entry.name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	kind := uint32(unix.S_IFREG)
	if entry.directory {
		kind = unix.S_IFDIR
	}
	if uint64(stat.Dev) != entry.device || uint64(stat.Ino) != entry.inode || uint32(stat.Mode)&unix.S_IFMT != kind {
		return errWriteUncertain
	}
	return nil
}

func (owner *writeStagingOwner) validate() error {
	for i, entry := range owner.entries {
		if err := entry.validateIdentity(); err != nil {
			return err
		}
		if entry.directory {
			if i+1 >= len(owner.entries) || owner.entries[i+1].parent != entry.file {
				return errWriteUncertain
			}
			if err := onlyWriteChild(entry.file, owner.entries[i+1].name); err != nil {
				return err
			}
		}
	}
	return nil
}

func onlyWriteChild(directory *os.File, name string) (resultErr error) {
	fd, err := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "write-staging-inspect")
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	names, err := file.Readdirnames(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(names) != 1 || names[0] != name {
		return errWriteUncertain
	}
	return nil
}

func (owner *writeStagingOwner) cleanup() error {
	if owner.hooks.beforeCleanup != nil {
		owner.hooks.beforeCleanup()
	}
	var errs []error
	for i := len(owner.entries) - 1; i >= 0; i-- {
		entry := owner.entries[i]
		if !owner.keep {
			if err := entry.validateIdentity(); err != nil {
				errs = append(errs, err)
			} else {
				var err error
				if owner.hooks.unlink != nil {
					err = owner.hooks.unlink(int(entry.parent.Fd()), entry.name, entry.directory)
				} else {
					flags := 0
					if entry.directory {
						flags = unix.AT_REMOVEDIR
					}
					err = unix.Unlinkat(int(entry.parent.Fd()), entry.name, flags)
				}
				errs = append(errs, err)
			}
		}
		// 注入 Close 故障不能放弃真实 fd 的关闭责任。
		if owner.hooks.close != nil {
			errs = append(errs, owner.hooks.close(entry.file))
		}
		errs = append(errs, entry.file.Close())
	}
	owner.entries = nil
	return errors.Join(errs...)
}

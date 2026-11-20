//go:build darwin || linux

package builtin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

var (
	errStagingIdentity  = errors.New("file staging identity changed")
	errStagingUncertain = errors.New("file staging ownership is uncertain")
)

type stagedEntry struct {
	parent    *os.File
	file      *os.File
	name      string
	device    uint64
	inode     uint64
	directory bool
}

// fileStagingOwner 只清理自己创建且身份仍匹配的项，不递归处理未知目录内容。
type fileStagingOwner struct {
	entries []stagedEntry
	keep    bool
}

type stagedContentHooks struct {
	write func(*os.File, []byte) error
	chmod func(*os.File, os.FileMode) error
	sync  func(*os.File) error
}

// prepareStagedContent 以此次创建的文件句柄写入、核对长度、设置权限并 Sync。
func prepareStagedContent(ctx context.Context, file *os.File, content []byte, mode uint32, hooks stagedContentHooks) error {
	var err error
	if hooks.write != nil {
		err = hooks.write(file, content)
	} else {
		err = writeStagedBytes(ctx, file, content)
	}
	if err != nil {
		return err
	}
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != int64(len(content)) {
		return io.ErrShortWrite
	}
	if hooks.chmod != nil {
		err = hooks.chmod(file, os.FileMode(mode))
	} else {
		err = file.Chmod(os.FileMode(mode))
	}
	if err != nil {
		return err
	}
	return syncStagingHandle(file, hooks.sync)
}

func writeStagedBytes(ctx context.Context, writer io.Writer, content []byte) error {
	for len(content) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := min(len(content), 64<<10)
		written, err := writer.Write(content[:length])
		if err != nil {
			return err
		}
		if written != length {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}

func syncStagingHandle(file *os.File, sync func(*os.File) error) error {
	if sync != nil {
		return sync(file)
	}
	return file.Sync()
}

func (owner *fileStagingOwner) createRandom(parent *os.File, prefix string, directory bool) (*os.File, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	return owner.create(parent, prefix+hex.EncodeToString(entropy[:]), directory)
}

func (owner *fileStagingOwner) create(parent *os.File, name string, directory bool) (*os.File, error) {
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
			return nil, errors.Join(err, errStagingUncertain)
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "file-staging")
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, errors.Join(err, file.Close(), errStagingUncertain)
	}
	entry := stagedEntry{parent: parent, file: file, name: name, device: uint64(stat.Dev), inode: uint64(stat.Ino), directory: directory}
	owner.entries = append(owner.entries, entry)
	if directory {
		// 显式恢复私有目录基本权限，避免进程 umask 导致后续无法进入。
		if err := file.Chmod(0o700); err != nil {
			return nil, err
		}
	}
	return file, nil
}

func (entry stagedEntry) kind() uint32 {
	if entry.directory {
		return unix.S_IFDIR
	}
	return unix.S_IFREG
}

func (entry stagedEntry) validateIdentity() error {
	var stat unix.Stat_t
	if err := unix.Fstatat(int(entry.parent.Fd()), entry.name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if uint64(stat.Dev) != entry.device || uint64(stat.Ino) != entry.inode || uint32(stat.Mode)&unix.S_IFMT != entry.kind() {
		return errStagingIdentity
	}
	return nil
}

func (owner *fileStagingOwner) validate(fileMode uint32) error {
	for i, entry := range owner.entries {
		if err := entry.validateIdentity(); err != nil {
			return err
		}
		mode := fileMode
		if entry.directory {
			if i+1 >= len(owner.entries) || owner.entries[i+1].parent != entry.file {
				return errStagingIdentity
			}
			if err := onlyStagedChild(entry.file, owner.entries[i+1].name); err != nil {
				return err
			}
			mode = 0o700
		}
		var stat unix.Stat_t
		if err := unix.Fstat(int(entry.file.Fd()), &stat); err != nil {
			return err
		}
		if uint32(stat.Mode)&0o7777 != mode {
			return errStagingIdentity
		}
	}
	return nil
}

func onlyStagedChild(directory *os.File, name string) (resultErr error) {
	fd, err := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "file-staging-inspect")
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	names, err := file.Readdirnames(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(names) != 1 || names[0] != name {
		return errStagingIdentity
	}
	return nil
}

func (owner *fileStagingOwner) cleanup(unlink func(int, string, bool) error, closeHook func(*os.File) error) error {
	var errs []error
	for i := len(owner.entries) - 1; i >= 0; i-- {
		entry := owner.entries[i]
		if !owner.keep {
			if err := entry.validateIdentity(); err != nil {
				errs = append(errs, err)
			} else {
				var err error
				if unlink != nil {
					err = unlink(int(entry.parent.Fd()), entry.name, entry.directory)
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
		if closeHook != nil {
			errs = append(errs, closeHook(entry.file))
		}
		errs = append(errs, entry.file.Close())
	}
	owner.entries = nil
	return errors.Join(errs...)
}

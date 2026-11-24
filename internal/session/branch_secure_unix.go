//go:build darwin || linux

package session

import (
	"errors"
	"fmt"
	"os"
	"path"

	"golang.org/x/sys/unix"
)

func (root *secureRoot) hooksForBranch() securePathHooks { return root.hooks }

func (root *secureRoot) syncBranchFile(file *os.File) error {
	if sync := root.hooks.syncBranchFile; sync != nil {
		return sync(file)
	}
	return file.Sync()
}

func (root *secureRoot) syncBranchDirectory(directory *os.File) error {
	if sync := root.hooks.syncBranchDirectory; sync != nil {
		return sync(directory)
	}
	return directory.Sync()
}

func (root *secureRoot) checkBranchRoute(relative string, directory *os.File, name string, file *os.File) error {
	// 日期路径每次都从原根句柄重新验证，禁止在被挪走的旧日期目录确认发布。
	rootFD, err := unix.Open(root.file.Name(), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("inspect canonical branch data root: %w", err)
	}
	canonicalRoot := os.NewFile(uintptr(rootFD), "branch-data-root")
	rootErr := sameBranchDirectory(root.file, canonicalRoot)
	if err := errors.Join(rootErr, canonicalRoot.Close()); err != nil {
		return err
	}
	canonicalDirectory, err := root.openDirectory(path.Dir(relative), false)
	if err != nil {
		return err
	}
	resultErr := sameBranchDirectory(directory, canonicalDirectory)
	if err := errors.Join(resultErr, canonicalDirectory.Close()); err != nil {
		return err
	}
	return checkBranchJournalEntry(directory, name, file)
}

func sameBranchDirectory(held, canonical *os.File) error {
	a, err := held.Stat()
	if err != nil {
		return err
	}
	b, err := canonical.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(a, b) || !a.IsDir() || a.Mode().Perm() != 0o700 {
		return fmt.Errorf("branch journal directory identity changed")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(held.Fd()), &stat); err != nil {
		return err
	}
	if stat.Uid != uint32(os.Geteuid()) || stat.Mode&0o7777 != 0o700 {
		return fmt.Errorf("branch journal directory ownership is unsafe")
	}
	return nil
}

func checkBranchJournalEntry(directory *os.File, name string, file *os.File) error {
	var entry, held unix.Stat_t
	if err := unix.Fstatat(int(directory.Fd()), name, &entry, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("inspect branch journal entry: %w", err)
	}
	if err := unix.Fstat(int(file.Fd()), &held); err != nil {
		return fmt.Errorf("inspect held branch journal: %w", err)
	}
	if entry.Dev != held.Dev || entry.Ino != held.Ino || held.Mode&0o170000 != unix.S_IFREG ||
		held.Mode&0o7777 != 0o600 || held.Uid != uint32(os.Geteuid()) || held.Nlink != 1 {
		return fmt.Errorf("branch journal entry identity or ownership changed")
	}
	return nil
}

func unlinkBranchStage(directory *os.File, name string) error {
	if err := unix.Unlinkat(int(directory.Fd()), name, 0); err != nil {
		return fmt.Errorf("remove branch staging journal: %w", err)
	}
	return nil
}

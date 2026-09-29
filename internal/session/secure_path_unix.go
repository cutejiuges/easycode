//go:build darwin || linux

package session

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type secureRoot struct {
	file  *os.File
	hooks securePathHooks
}

func openOrCreateSecureRoot(rootPath string, hooks securePathHooks) (*secureRoot, error) {
	parentPath := filepath.Dir(rootPath)
	if err := os.MkdirAll(parentPath, 0o700); err != nil {
		return nil, fmt.Errorf("create session data root parent: %w", err)
	}
	parentFD, err := unix.Open(parentPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, fmt.Errorf("open session data root parent: %w", err)
	}
	parent := os.NewFile(uintptr(parentFD), parentPath)
	defer func() { _ = parent.Close() }()

	base := filepath.Base(rootPath)
	callSecurePathHook(hooks.beforeOpenRoot, rootPath)
	rootFD, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	created := false
	if errors.Is(err, fs.ErrNotExist) {
		if mkdirErr := unix.Mkdirat(parentFD, base, 0o700); mkdirErr != nil {
			return nil, fmt.Errorf("create session data root: %w", mkdirErr)
		}
		created = true
		rootFD, err = unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("session data root must be a real directory: %w", err)
	}
	rootFile := os.NewFile(uintptr(rootFD), rootPath)
	info, statErr := rootFile.Stat()
	if statErr != nil {
		_ = rootFile.Close()
		return nil, fmt.Errorf("inspect session data root: %w", statErr)
	}
	if !info.IsDir() {
		_ = rootFile.Close()
		return nil, fmt.Errorf("session data root must be a real directory")
	}
	if permissionErr := validatePrivatePermissions(info, 0o700, "session data root"); permissionErr != nil {
		_ = rootFile.Close()
		return nil, permissionErr
	}
	if created {
		if syncErr := parent.Sync(); syncErr != nil {
			_ = rootFile.Close()
			return nil, fmt.Errorf("sync session data root parent: %w", syncErr)
		}
	}
	callSecurePathHook(hooks.afterOpenRoot, rootPath)
	return &secureRoot{file: rootFile, hooks: hooks}, nil
}

func (root *secureRoot) openDirectory(relative string, create bool) (*os.File, error) {
	if root == nil || root.file == nil {
		return nil, fmt.Errorf("session data root is closed")
	}
	currentFD, err := unix.Dup(int(root.file.Fd()))
	if err != nil {
		return nil, fmt.Errorf("duplicate session data root handle: %w", err)
	}
	unix.CloseOnExec(currentFD)
	current := os.NewFile(uintptr(currentFD), root.file.Name())
	currentRelative := ""
	for _, component := range splitPath(relative) {
		if currentRelative == "" {
			currentRelative = component
		} else {
			currentRelative = path.Join(currentRelative, component)
		}
		callSecurePathHook(root.hooks.beforeOpenComponent, currentRelative)
		nextFD, openErr := unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		created := false
		if create && errors.Is(openErr, fs.ErrNotExist) {
			if mkdirErr := unix.Mkdirat(currentFD, component, 0o700); mkdirErr != nil {
				_ = current.Close()
				return nil, fmt.Errorf("create session directory: %w", mkdirErr)
			}
			created = true
			nextFD, openErr = unix.Openat(currentFD, component, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		}
		if openErr != nil {
			_ = current.Close()
			return nil, fmt.Errorf("session directory must be a real directory: %w", openErr)
		}
		next := os.NewFile(uintptr(nextFD), currentRelative)
		info, statErr := next.Stat()
		if statErr != nil || !info.IsDir() {
			_ = next.Close()
			_ = current.Close()
			if statErr != nil {
				return nil, fmt.Errorf("inspect session directory: %w", statErr)
			}
			return nil, fmt.Errorf("session directory must be a real directory")
		}
		if permissionErr := validatePrivatePermissions(info, 0o700, "session directory"); permissionErr != nil {
			_ = next.Close()
			_ = current.Close()
			return nil, permissionErr
		}
		if created {
			if syncErr := current.Sync(); syncErr != nil {
				_ = next.Close()
				_ = current.Close()
				return nil, fmt.Errorf("sync session directory parent: %w", syncErr)
			}
		}
		_ = current.Close()
		current = next
		currentFD = nextFD
		callSecurePathHook(root.hooks.afterOpenComponent, currentRelative)
	}
	return current, nil
}

func (root *secureRoot) openJournal(
	directory *os.File,
	name string,
	flags int,
	permissions fs.FileMode,
) (*os.File, error) {
	if root == nil || directory == nil || name == "" || path.Base(name) != name {
		return nil, fmt.Errorf("session journal path is invalid")
	}
	relative := path.Join(directory.Name(), name)
	callSecurePathHook(root.hooks.beforeOpenJournal, relative)
	fd, err := unix.Openat(
		int(directory.Fd()), name,
		flags|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		uint32(permissions.Perm()),
	)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EISDIR) {
			return nil, fmt.Errorf("session journal must be a regular file: %w", err)
		}
		return nil, fmt.Errorf("open session journal: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	callSecurePathHook(root.hooks.afterOpenJournal, relative)
	return file, nil
}

func (root *secureRoot) Close() error {
	if root == nil || root.file == nil {
		return nil
	}
	file := root.file
	root.file = nil
	return file.Close()
}

func callSecurePathHook(hook func(string), value string) {
	if hook != nil {
		hook(value)
	}
}

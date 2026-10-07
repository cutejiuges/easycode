//go:build darwin || linux

package builtin

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

type workspaceHandle interface {
	openFile(string, workspaceHooks) (*os.File, error)
	close() error
}

type unixWorkspaceHandle struct{ root *os.File }

func openWorkspaceHandle(path string) (workspaceHandle, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "workspace")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("workspace handle is unavailable")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = file.Close()
		return nil, errors.New("workspace is not a directory")
	}
	return &unixWorkspaceHandle{root: file}, nil
}

func (handle *unixWorkspaceHandle) close() error {
	if handle == nil || handle.root == nil {
		return nil
	}
	return handle.root.Close()
}

func (handle *unixWorkspaceHandle) openFile(relative string, hooks workspaceHooks) (*os.File, error) {
	components := strings.Split(relative, "/")
	currentFD, err := unix.Dup(int(handle.root.Fd()))
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(currentFD), "workspace-component")
	if current == nil {
		_ = unix.Close(currentFD)
		return nil, errors.New("workspace component handle is unavailable")
	}
	for index, component := range components {
		if hooks.beforeOpenComponent != nil {
			hooks.beforeOpenComponent(strings.Join(components[:index+1], "/"))
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if index < len(components)-1 {
			flags |= unix.O_DIRECTORY
		}
		nextFD, openErr := unix.Openat(int(current.Fd()), component, flags, 0)
		if openErr != nil {
			_ = current.Close()
			if errors.Is(openErr, unix.EACCES) {
				return nil, errTargetNotReadable
			}
			return nil, openErr
		}
		next := os.NewFile(uintptr(nextFD), "workspace-file")
		if next == nil {
			_ = unix.Close(nextFD)
			_ = current.Close()
			return nil, errors.New("workspace file handle is unavailable")
		}
		if hooks.afterOpenComponent != nil {
			hooks.afterOpenComponent(strings.Join(components[:index+1], "/"))
		}
		if closeErr := current.Close(); closeErr != nil {
			_ = next.Close()
			return nil, closeErr
		}
		current = next
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(current.Fd()), &stat); err != nil {
		_ = current.Close()
		return nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		_ = current.Close()
		return nil, errTargetNotRegular
	}
	if stat.Mode&0o444 == 0 {
		_ = current.Close()
		return nil, errTargetNotReadable
	}
	if stat.Size < 0 || stat.Size > maxReadFileBytes {
		_ = current.Close()
		return nil, errTargetTooLarge
	}
	return current, nil
}

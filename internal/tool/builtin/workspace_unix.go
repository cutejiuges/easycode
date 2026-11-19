//go:build darwin || linux

package builtin

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

type unixWorkspaceHandle struct{ root *os.File }

type unixWorkspaceNode struct {
	handle       *os.File
	relativePath string
	nodeKind     workspaceNodeKind
	fileSize     int64
}

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

func (handle *unixWorkspaceHandle) inspectRoot() (os.FileInfo, error) {
	return handle.root.Stat()
}

func (handle *unixWorkspaceHandle) openDirectory(relative string, hooks workspaceHooks) (workspaceDirectory, error) {
	if handle == nil || handle.root == nil {
		return nil, errors.New("workspace is closed")
	}
	rootFD, err := openWorkspaceRelative(handle.root, ".", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, nil)
	if err != nil {
		return nil, err
	}
	root := os.NewFile(uintptr(rootFD), "workspace-directory")
	if root == nil {
		_ = unix.Close(rootFD)
		return nil, errors.New("workspace directory handle is unavailable")
	}
	current := &unixWorkspaceNode{handle: root, nodeKind: workspaceNodeDirectory}
	if relative == "" {
		return current, nil
	}
	for _, component := range strings.Split(relative, "/") {
		child, openErr := current.openChild(component, hooks)
		if openErr != nil {
			_ = current.close()
			return nil, openErr
		}
		if child.kind() != workspaceNodeDirectory {
			_ = child.close()
			_ = current.close()
			return nil, errTargetNotRegular
		}
		next := child.directory()
		if closeErr := current.close(); closeErr != nil {
			_ = next.close()
			return nil, closeErr
		}
		current = next.(*unixWorkspaceNode)
	}
	return current, nil
}

func (handle *unixWorkspaceHandle) openFile(relative string, hooks workspaceHooks) (*os.File, error) {
	components := strings.Split(relative, "/")
	parentPath := strings.Join(components[:len(components)-1], "/")
	directory, err := handle.openDirectory(parentPath, hooks)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.close() }()
	child, err := directory.openChild(components[len(components)-1], hooks)
	if err != nil {
		return nil, err
	}
	if child.kind() != workspaceNodeRegular {
		_ = child.close()
		return nil, errTargetNotRegular
	}
	if child.size() > maxReadFileBytes {
		_ = child.close()
		return nil, errTargetTooLarge
	}
	file := child.file()
	node := child.(*unixWorkspaceNode)
	node.handle = nil
	return file, nil
}

func (node *unixWorkspaceNode) readEntryNames(batch int) ([]string, error) {
	if node == nil || node.handle == nil || node.nodeKind != workspaceNodeDirectory {
		return nil, errors.New("workspace directory is unavailable")
	}
	if batch < 1 || batch > workspaceNameBatch {
		return nil, errors.New("workspace directory batch is invalid")
	}
	return node.handle.Readdirnames(batch)
}

// interrupt 只关闭实际 FD；指针状态由原读取栈在回调完成后清理。
func (node *unixWorkspaceNode) interrupt() error { return node.handle.Close() }

func (node *unixWorkspaceNode) openChild(name string, hooks workspaceHooks) (workspaceChild, error) {
	if node == nil || node.handle == nil || node.nodeKind != workspaceNodeDirectory || name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.ContainsRune(name, 0) {
		return nil, errors.New("workspace child name is invalid")
	}
	relative := name
	if node.relativePath != "" {
		relative = node.relativePath + "/" + name
	}
	fd, err := openWorkspaceRelative(node.handle, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, func() {
		if hooks.beforeOpenComponent != nil {
			hooks.beforeOpenComponent(relative)
		}
	})
	if err != nil {
		if errors.Is(err, unix.EACCES) {
			return nil, errTargetNotReadable
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "workspace-child")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("workspace child handle is unavailable")
	}
	if hooks.afterOpenComponent != nil {
		hooks.afterOpenComponent(relative)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = file.Close()
		return nil, err
	}
	kind := workspaceNodeUnsupported
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		kind = workspaceNodeDirectory
	case unix.S_IFREG:
		kind = workspaceNodeRegular
		if stat.Mode&0o444 == 0 {
			_ = file.Close()
			return nil, errTargetNotReadable
		}
	}
	return &unixWorkspaceNode{handle: file, relativePath: relative, nodeKind: kind, fileSize: stat.Size}, nil
}

// openWorkspaceRelative 在整个 openat 期间借用目录 FD，取消关闭不能让原数字被其他对象复用。
func openWorkspaceRelative(directory *os.File, name string, flags int, beforeOpen func()) (int, error) {
	connection, err := directory.SyscallConn()
	if err != nil {
		return -1, err
	}
	fd := -1
	var openErr error
	err = connection.Control(func(parent uintptr) {
		if beforeOpen != nil {
			beforeOpen()
		}
		fd, openErr = unix.Openat(int(parent), name, flags, 0)
	})
	if err != nil {
		return -1, err
	}
	return fd, openErr
}

func (node *unixWorkspaceNode) kind() workspaceNodeKind { return node.nodeKind }
func (node *unixWorkspaceNode) size() int64             { return node.fileSize }
func (node *unixWorkspaceNode) file() *os.File          { return node.handle }
func (node *unixWorkspaceNode) directory() workspaceDirectory {
	if node.nodeKind != workspaceNodeDirectory {
		return nil
	}
	return node
}
func (node *unixWorkspaceNode) close() error {
	if node == nil || node.handle == nil {
		return nil
	}
	err := node.handle.Close()
	node.handle = nil
	return err
}

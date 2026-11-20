//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"os"
	"strings"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

type unixPatchFile struct {
	workspace *unixWorkspaceHandle
	parent    *unixWorkspaceNode
	target    *os.File
	name      string
	stat      unix.Stat_t
}

func (handle *unixWorkspaceHandle) openPatchFile(relative string, hooks workspaceHooks) (patchFile, error) {
	components := strings.Split(relative, "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || strings.ContainsRune(component, 0) || strings.Contains(component, "\\") {
			return nil, errTargetNotRegular
		}
	}
	directory, err := handle.openDirectory(strings.Join(components[:len(components)-1], "/"), hooks)
	if err != nil {
		return nil, err
	}
	parent := directory.(*unixWorkspaceNode)
	name := components[len(components)-1]
	if hooks.beforeOpenComponent != nil {
		hooks.beforeOpenComponent(relative)
	}
	fd, err := unix.Openat(int(parent.handle.Fd()), name, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.EACCES) {
			err = errTargetNotReadable
		}
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EISDIR) {
			err = errTargetNotRegular
		}
		_ = parent.close()
		return nil, err
	}
	target := os.NewFile(uintptr(fd), "workspace-patch-target")
	if target == nil {
		_ = unix.Close(fd)
		_ = parent.close()
		return nil, errTargetNotRegular
	}
	if hooks.afterOpenComponent != nil {
		hooks.afterOpenComponent(relative)
	}
	var stat unix.Stat_t
	if err = unix.Fstat(fd, &stat); err == nil {
		switch {
		case stat.Mode&unix.S_IFMT != unix.S_IFREG:
			err = errTargetNotRegular
		case stat.Mode&0o444 == 0 || stat.Mode&0o222 == 0 || stat.Mode&0o7000 != 0:
			err = errTargetNotReadable
		case stat.Size > maxReadFileBytes:
			err = errTargetTooLarge
		}
	}
	if err != nil {
		_ = target.Close()
		_ = parent.close()
		return nil, err
	}
	return &unixPatchFile{workspace: handle, parent: parent, target: target, name: name, stat: stat}, nil
}

func (target *unixPatchFile) file() *os.File { return target.target }

func (target *unixPatchFile) evidence(content []byte) (tool.FileBaseEvidence, error) {
	var current unix.Stat_t
	if err := unix.Fstat(int(target.target.Fd()), &current); err != nil {
		return tool.FileBaseEvidence{}, err
	}
	if current.Dev != target.stat.Dev || current.Ino != target.stat.Ino || current.Mode != target.stat.Mode || current.Size != target.stat.Size || current.Size != int64(len(content)) {
		return tool.FileBaseEvidence{}, errors.New("patch base changed while reading")
	}
	return tool.NewFileBaseEvidence(tool.NewContentDigest(content), current.Size, uint32(current.Mode&0o777), uint64(current.Dev), uint64(current.Ino))
}

func (target *unixPatchFile) close() error {
	return errors.Join(target.target.Close(), target.parent.close())
}

// replace 持有已核验父目录直到暂存清理结束。rename 是提交线性化点。
func (target *unixPatchFile) replace(ctx context.Context, expected tool.FileBaseEvidence, final []byte, hooks patchHooks) (committed bool, resultErr error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	owner := fileStagingOwner{}
	defer func() {
		var unlink func(int, string, bool) error
		if hooks.cleanup != nil {
			unlink = func(parent int, name string, _ bool) error { return hooks.cleanup(parent, name) }
		}
		if cleanupErr := owner.cleanup(unlink, nil); cleanupErr != nil || errors.Is(resultErr, errStagingUncertain) {
			resultErr = errors.Join(resultErr, cleanupErr, errPatchCleanupUncertain)
		}
	}()
	temporary, err := owner.createRandom(target.parent.handle, ".easycode-patch-", false)
	if err != nil {
		return false, err
	}
	mode := uint32(target.stat.Mode & 0o777)
	if err = prepareStagedContent(ctx, temporary, final, mode, stagedContentHooks{write: hooks.write, sync: hooks.fileSync}); err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if hooks.beforeRename != nil {
		hooks.beforeRename()
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if err = target.revalidateCurrent(ctx, expected); err != nil {
		return false, err
	}
	if err = owner.validate(mode); err != nil {
		return false, errors.Join(err, errPatchConflict)
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	parentFD := int(target.parent.handle.Fd())
	name := owner.entries[0].name
	if hooks.rename != nil {
		err = hooks.rename(parentFD, name, target.name)
	} else {
		err = unix.Renameat(parentFD, name, parentFD, target.name)
	}
	if err != nil {
		return false, err
	}
	owner.keep = true
	// rename 已完成，取消不能跳过目录 Sync，也不能覆盖已确定持久化的结果。
	return true, syncStagingHandle(target.parent.handle, hooks.directorySync)
}

func (target *unixPatchFile) revalidateCurrent(ctx context.Context, expected tool.FileBaseEvidence) error {
	directory, err := target.workspace.openDirectory(target.parent.relativePath, workspaceHooks{})
	if err != nil {
		return errPatchConflict
	}
	parent := directory.(*unixWorkspaceNode)
	defer func() { _ = parent.close() }()
	before, err := directoryEvidence(target.parent.handle, ".")
	if err != nil {
		return errPatchConflict
	}
	after, err := directoryEvidence(parent.handle, ".")
	if err != nil || before != after {
		return errPatchConflict
	}
	fd, err := unix.Openat(int(parent.handle.Fd()), target.name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return errPatchConflict
	}
	current := os.NewFile(uintptr(fd), "workspace-patch-revalidate")
	if current == nil {
		_ = unix.Close(fd)
		return errPatchConflict
	}
	defer func() { _ = current.Close() }()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size > maxReadFileBytes {
		return errPatchConflict
	}
	content, code, _ := readBounded(ctx, current)
	if code == "cancelled" {
		return ctx.Err()
	}
	if code != "" {
		return errPatchConflict
	}
	evidence, err := tool.NewFileBaseEvidence(tool.NewContentDigest(content), stat.Size, uint32(stat.Mode&0o777), uint64(stat.Dev), uint64(stat.Ino))
	if err != nil || evidence != expected {
		return errPatchConflict
	}
	return nil
}

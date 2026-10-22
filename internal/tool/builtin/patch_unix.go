//go:build darwin || linux

package builtin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

type unixPatchFile struct {
	parent *unixWorkspaceNode
	target *os.File
	name   string
	stat   unix.Stat_t
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
	return &unixPatchFile{parent: parent, target: target, name: name, stat: stat}, nil
}

func (target *unixPatchFile) file() *os.File { return target.target }

func (target *unixPatchFile) evidence(content []byte) (tool.PatchBaseEvidence, error) {
	var current unix.Stat_t
	if err := unix.Fstat(int(target.target.Fd()), &current); err != nil {
		return tool.PatchBaseEvidence{}, err
	}
	if current.Dev != target.stat.Dev || current.Ino != target.stat.Ino || current.Mode != target.stat.Mode || current.Size != target.stat.Size || current.Size != int64(len(content)) {
		return tool.PatchBaseEvidence{}, errors.New("patch base changed while reading")
	}
	return tool.NewPatchBaseEvidence(tool.NewContentDigest(content), current.Size, uint32(current.Mode&0o777), uint64(current.Dev), uint64(current.Ino))
}

func (target *unixPatchFile) close() error {
	return errors.Join(target.target.Close(), target.parent.close())
}

// replace 的唯一临时文件owner持有父目录直到cleanup结束；rename是提交线性化点。
func (target *unixPatchFile) replace(ctx context.Context, expected tool.PatchBaseEvidence, final []byte, hooks patchHooks) (committed bool, resultErr error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return false, errors.New("patch temporary identity unavailable")
	}
	name := ".easycode-patch-" + hex.EncodeToString(entropy[:])
	parentFD := int(target.parent.handle.Fd())
	fd, err := unix.Openat(parentFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return false, err
	}
	temporary := os.NewFile(uintptr(fd), "workspace-patch-temporary")
	if temporary == nil {
		_ = unix.Close(fd)
		_ = unix.Unlinkat(parentFD, name, 0)
		return false, errors.New("patch temporary handle unavailable")
	}
	defer func() {
		closeErr := temporary.Close()
		var cleanupErr error
		if !committed {
			if hooks.cleanup != nil {
				cleanupErr = hooks.cleanup(parentFD, name)
			} else {
				cleanupErr = unix.Unlinkat(parentFD, name, 0)
			}
		}
		resultErr = errors.Join(resultErr, closeErr, cleanupErr)
	}()
	if hooks.write != nil {
		err = hooks.write(temporary, final)
	} else {
		err = writePatchBytes(ctx, temporary, final)
	}
	if err != nil {
		return false, err
	}
	var temporaryStat unix.Stat_t
	if err = unix.Fstat(fd, &temporaryStat); err != nil || temporaryStat.Size != int64(len(final)) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return false, err
	}
	if err = temporary.Chmod(os.FileMode(target.stat.Mode & 0o777)); err != nil {
		return false, err
	}
	if hooks.fileSync != nil {
		err = hooks.fileSync(temporary)
	} else {
		err = temporary.Sync()
	}
	if err != nil {
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
	if hooks.rename != nil {
		err = hooks.rename(parentFD, name, target.name)
	} else {
		err = unix.Renameat(parentFD, name, parentFD, target.name)
	}
	if err != nil {
		return false, err
	}
	committed = true
	// 提交后不让取消绕过目录Sync；Sync失败或随后的取消必须诚实报告不确定结果。
	if hooks.directorySync != nil {
		err = hooks.directorySync(target.parent.handle)
	} else {
		err = target.parent.handle.Sync()
	}
	if err != nil {
		return true, err
	}
	if err = ctx.Err(); err != nil {
		return true, err
	}
	return true, nil
}

func (target *unixPatchFile) revalidateCurrent(ctx context.Context, expected tool.PatchBaseEvidence) error {
	fd, err := unix.Openat(int(target.parent.handle.Fd()), target.name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
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
	evidence, err := tool.NewPatchBaseEvidence(tool.NewContentDigest(content), stat.Size, uint32(stat.Mode&0o777), uint64(stat.Dev), uint64(stat.Ino))
	if err != nil || evidence != expected {
		return errPatchConflict
	}
	return nil
}

func writePatchBytes(ctx context.Context, writer io.Writer, content []byte) error {
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

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

// unixWritePath 持有只读打开的祖先与目标；每个句柄只有本对象负责关闭。
type unixWritePath struct {
	workspace   *unixWorkspaceHandle
	relative    string
	hooks       workspaceHooks
	parents     []*os.File
	ancestors   []tool.DirectoryEvidence
	directories []string
	target      *os.File
}

func (handle *unixWorkspaceHandle) openWritePath(relative string, hooks workspaceHooks) (result writePath, resultErr error) {
	if _, err := tool.NewWriteInput(relative, ""); err != nil {
		return nil, errTargetNotRegular
	}
	p := &unixWritePath{workspace: handle, relative: relative, hooks: hooks}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, p.close())
		}
	}()
	fd, err := unix.Openat(int(handle.root.Fd()), ".", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	root := os.NewFile(uintptr(fd), "write-root")
	p.parents = append(p.parents, root)
	evidence, err := directoryEvidence(root, ".")
	if err != nil {
		return nil, err
	}
	p.ancestors = append(p.ancestors, evidence)
	parts := strings.Split(relative, "/")
	for i, part := range parts {
		path := strings.Join(parts[:i+1], "/")
		if hooks.beforeOpenComponent != nil {
			hooks.beforeOpenComponent(path)
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		childFD, err := unix.Openat(int(p.parents[len(p.parents)-1].Fd()), part, flags, 0)
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				for j := i; j < len(parts)-1; j++ {
					p.directories = append(p.directories, strings.Join(parts[:j+1], "/"))
				}
				if len(p.directories) > tool.MaxWriteDirectories {
					return nil, writeError("write_directory_limit")
				}
				return p, nil
			}
			if errors.Is(err, unix.EACCES) {
				return nil, errTargetNotReadable
			}
			if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
				return nil, errTargetNotRegular
			}
			return nil, err
		}
		file := os.NewFile(uintptr(childFD), "write-path")
		if i == len(parts)-1 {
			p.target = file
		} else {
			p.parents = append(p.parents, file)
		}
		if hooks.afterOpenComponent != nil {
			hooks.afterOpenComponent(path)
		}
		if i < len(parts)-1 {
			evidence, err := directoryEvidence(file, path)
			if err != nil {
				return nil, err
			}
			p.ancestors = append(p.ancestors, evidence)
		} else {
			state, err := readFileSnapshot(file)
			if err != nil {
				return nil, err
			}
			if state.mode&0o7000 != 0 || state.mode&0o444 == 0 || state.mode&0o222 == 0 {
				return nil, errTargetNotReadable
			}
			if state.size > maxReadFileBytes {
				return nil, errTargetTooLarge
			}
		}
	}
	return p, nil
}

func directoryEvidence(file *os.File, path string) (tool.DirectoryEvidence, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return tool.DirectoryEvidence{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return tool.DirectoryEvidence{}, errTargetNotRegular
	}
	return tool.NewDirectoryEvidence(path, uint64(stat.Dev), uint64(stat.Ino), uint32(stat.Mode&0o7777))
}

func (p *unixWritePath) inspect(ctx context.Context) (writeInspection, error) {
	state := writeInspection{operation: tool.WriteCreate, ancestors: append([]tool.DirectoryEvidence(nil), p.ancestors...), directories: append([]string(nil), p.directories...)}
	if p.target == nil {
		return state, nil
	}
	state.operation = tool.WriteOverwrite
	before, err := readFileSnapshot(p.target)
	if err != nil {
		return state, err
	}
	content, code, _ := readBounded(ctx, p.target)
	if code != "" {
		if ctx.Err() != nil {
			return state, ctx.Err()
		}
		return state, errTargetNotReadable
	}
	after, err := readFileSnapshot(p.target)
	if err != nil || before != after || after.size != int64(len(content)) {
		return state, errWriteConflict
	}
	state.base, err = tool.NewFileBaseEvidence(tool.NewContentDigest(content), after.size, after.mode, after.device, after.inode)
	state.content = content
	return state, err
}

func (p *unixWritePath) close() error {
	var errs []error
	if p.target != nil {
		errs = append(errs, p.target.Close())
		p.target = nil
	}
	for i := len(p.parents) - 1; i >= 0; i-- {
		errs = append(errs, p.parents[i].Close())
	}
	p.parents = nil
	return errors.Join(errs...)
}

func (p *unixWritePath) revalidate(ctx context.Context, c tool.WriteCandidate) (resultErr error) {
	fresh, err := p.workspace.openWritePath(p.relative, p.hooks)
	if err != nil {
		return errWriteConflict
	}
	defer func() {
		if fresh.close() != nil {
			resultErr = errors.Join(resultErr, errWriteUncertain)
		}
	}()
	state, err := fresh.inspect(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || !matchesWriteCandidate(state, c) {
		return errWriteConflict
	}
	return nil
}

func (p *unixWritePath) publish(ctx context.Context, c tool.WriteCandidate, hooks writeHooks) (resultErr error) {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	owner := writeStagingOwner{hooks: hooks}
	defer func() {
		if err := owner.cleanup(); err != nil {
			resultErr = errors.Join(resultErr, errWriteUncertain)
		}
	}()
	parent := p.parents[len(p.parents)-1]
	parts := strings.Split(p.relative, "/")
	publishTo := parts[len(p.ancestors)-1]
	var stagedFile *os.File
	var err error
	if len(p.directories) == 0 {
		stagedFile, err = owner.createRandom(parent, false)
	} else {
		var directory *os.File
		directory, err = owner.createRandom(parent, true)
		for i := len(p.ancestors); err == nil && i < len(parts)-1; i++ {
			directory, err = owner.create(directory, parts[i], true)
		}
		if err == nil {
			stagedFile, err = owner.create(directory, parts[len(parts)-1], false)
		}
	}
	if err != nil {
		return err
	}
	if hooks.write != nil {
		err = hooks.write(stagedFile, c.FinalBytes())
	} else {
		err = writePatchBytes(ctx, stagedFile, c.FinalBytes())
	}
	if err != nil {
		return err
	}
	stat, err := stagedFile.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != int64(len(c.FinalBytes())) {
		return errors.New("short Write staging file")
	}
	if hooks.chmod != nil {
		err = hooks.chmod(stagedFile, os.FileMode(c.Mode()))
	} else {
		err = stagedFile.Chmod(os.FileMode(c.Mode()))
	}
	if err != nil {
		return err
	}
	if hooks.fileSync != nil {
		err = hooks.fileSync(stagedFile)
	} else {
		err = stagedFile.Sync()
	}
	if err != nil {
		return err
	}
	for i := len(owner.entries) - 1; i >= 0; i-- {
		entry := owner.entries[i]
		if entry.directory {
			if err = syncWriteDirectory(entry.file, hooks); err != nil {
				return err
			}
		}
	}
	if hooks.beforePublish != nil {
		hooks.beforePublish()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err = p.revalidate(ctx, c); err != nil {
		return err
	}
	// 发布前核验整个暂存清单，防止名称被替换后发布未知对象。
	if err = owner.validate(); err != nil {
		return errors.Join(err, errWriteUncertain)
	}
	parentFD := int(parent.Fd())
	name := owner.entries[0].name
	noReplace := c.Operation() == tool.WriteCreate
	if hooks.publish != nil {
		err = hooks.publish(parentFD, name, publishTo, noReplace)
	} else {
		err = publishWriteEntry(parentFD, name, publishTo, noReplace)
	}
	if err != nil {
		classified := classifyWritePublishError(err)
		if errors.Is(classified, errWriteUncertain) {
			owner.keep = true
		}
		return classified
	}
	owner.keep = true
	if hooks.afterPublish != nil {
		hooks.afterPublish()
	}
	// 发布已确定成功，取消不能跳过目录持久化或资源关闭。
	if err = syncWriteDirectory(parent, hooks); err != nil {
		return errors.Join(err, errWriteUncertain)
	}
	return nil
}

func syncWriteDirectory(file *os.File, hooks writeHooks) error {
	if hooks.directorySync != nil {
		return hooks.directorySync(file)
	}
	return file.Sync()
}

func publishWriteEntry(parent int, from, to string, noReplace bool) error {
	if noReplace {
		return renameWriteNoReplace(parent, from, to)
	}
	return unix.Renameat(parent, from, parent, to)
}

func classifyWritePublishError(err error) error {
	switch {
	case errors.Is(err, unix.EEXIST), errors.Is(err, unix.ENOTEMPTY):
		return errWriteConflict
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EINVAL):
		return errWriteUnsupported
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM), errors.Is(err, unix.EROFS), errors.Is(err, unix.ENOSPC), errors.Is(err, unix.EDQUOT), errors.Is(err, unix.EXDEV), errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENOTDIR), errors.Is(err, unix.EISDIR):
		return err
	default:
		return errors.Join(err, errWriteUncertain)
	}
}

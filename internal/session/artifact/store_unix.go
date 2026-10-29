//go:build darwin || linux

package artifact

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"

	"easycode/internal/domain"
	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

// Store 持有一个线程的输出目录；生命周期由持有该线程 journal lease 的宿主拥有。
type Store struct {
	root, thread             *os.File
	threadID                 domain.ThreadID
	threadBudget, rootBudget int64
}

// Open 在已校验的存储布局下打开线程目录；调用方必须已取得 journal lease。
func Open(ctx context.Context, layout *Layout, thread domain.ThreadID) (*Store, error) {
	if !thread.Valid() || layout.Check(ctx) != nil {
		return nil, errUnsafeLayout
	}
	root, err := openChildDirectory(layout.storage, "artifacts", true)
	if err != nil {
		return nil, err
	}
	dir, err := openChildDirectory(root, string(thread), true)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	return &Store{root: root, thread: dir, threadID: thread, threadBudget: tool.ArtifactThreadBytes, rootBudget: tool.ArtifactRootBytes}, nil
}

// Close 只允许在所有保存及回读操作完成后调用。
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.thread != nil {
		err = errors.Join(err, s.thread.Close())
		s.thread = nil
	}
	if s.root != nil {
		err = errors.Join(err, s.root.Close())
		s.root = nil
	}
	return err
}

func openChildDirectory(parent *os.File, name string, create bool) (*os.File, error) {
	if parent == nil {
		return nil, tool.ArtifactReadUnavailable
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_DIRECTORY | unix.O_NOFOLLOW
	fd, err := unix.Openat(int(parent.Fd()), name, flags, 0)
	if create && errors.Is(err, unix.ENOENT) {
		if err = unix.Mkdirat(int(parent.Fd()), name, 0o700); err != nil {
			return nil, err
		}
		if err = parent.Sync(); err != nil {
			return nil, err
		}
		fd, err = unix.Openat(int(parent.Fd()), name, flags, 0)
	}
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "artifact-directory")
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o777 != 0o700 || stat.Uid != uint32(os.Geteuid()) {
		_ = f.Close()
		return nil, tool.ArtifactReadUnavailable
	}
	return f, nil
}

func openObject(parent *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "artifact-object")
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o777 != 0o600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		_ = f.Close()
		return nil, tool.ArtifactReadUnavailable
	}
	return f, nil
}

func names(dir *os.File) ([]string, error) {
	f, err := openChildDirectory(dir, ".", false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return f.Readdirnames(-1)
}

func lockAccounting(ctx context.Context, root *os.File) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := openObject(root, "accounting", unix.O_RDWR|unix.O_CREAT)
	if err != nil {
		return nil, err
	}
	for {
		if ctx.Err() != nil {
			_ = f.Close()
			return nil, ctx.Err()
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = f.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Start 原子预留整次命令的预算，返回独占保存 owner。
func (s *Store) Start(ctx context.Context, id tool.InvocationID) (*Capture, error) {
	if ctx == nil || !id.Valid() || s == nil || s.root == nil {
		return nil, tool.ArtifactReadUnavailable
	}
	guard, err := lockAccounting(ctx, s.root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = guard.Close() }()
	total, thread, err := s.usage(ctx)
	if err != nil {
		return nil, tool.ArtifactReadUnavailable
	}
	if total+tool.ArtifactInvocationBytes > s.rootBudget || thread+tool.ArtifactInvocationBytes > s.threadBudget {
		return nil, tool.ArtifactQuotaExceeded
	}
	// 创建调用目录是预留接收点；之前取消不创建调用资源，之后返回确定 owner 或失败。
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// invocation 目录只创建一次，重放不能重新获得执行存储。
	if err = unix.Mkdirat(int(s.thread.Fd()), string(id), 0o700); err != nil {
		return nil, tool.ArtifactReadUnavailable
	}
	dir, err := openChildDirectory(s.thread, string(id), false)
	if err != nil {
		return nil, err
	}
	reservation, err := openObject(dir, "reservation", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	if err = unix.Flock(int(reservation.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		_, err = reservation.WriteString("33554432\n")
	}
	if err == nil {
		err = reservation.Sync()
	}
	if err == nil {
		err = dir.Sync()
	}
	if err == nil {
		err = s.thread.Sync()
	}
	if err != nil {
		_ = reservation.Close()
		_ = dir.Close()
		return nil, tool.ArtifactReadUnavailable
	}
	c := &Capture{directory: dir, reservation: reservation, thread: s.threadID, invocation: id, syncDirectory: syncArtifactDirectory, publish: publishObject}
	if err = c.openStreams(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func syncArtifactDirectory(file *os.File) error { return file.Sync() }

func (s *Store) usage(ctx context.Context) (int64, int64, error) {
	threads, err := names(s.root)
	if err != nil {
		return 0, 0, err
	}
	var total, current int64
	for _, name := range threads {
		if name == "accounting" {
			continue
		}
		if ctx.Err() != nil {
			return 0, 0, ctx.Err()
		}
		id, err := domain.ParseThreadID(name)
		if err != nil {
			return 0, 0, err
		}
		dir, err := openChildDirectory(s.root, name, false)
		if err != nil {
			return 0, 0, err
		}
		n, err := threadUsage(dir)
		_ = dir.Close()
		if err != nil {
			return 0, 0, err
		}
		total += n
		if id == s.threadID {
			current = n
		}
	}
	return total, current, nil
}

func threadUsage(thread *os.File) (int64, error) {
	entries, err := names(thread)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		if _, err = tool.ParseInvocationID(entry); err != nil {
			return 0, err
		}
		dir, err := openChildDirectory(thread, entry, false)
		if err != nil {
			return 0, err
		}
		n, err := invocationUsage(dir)
		_ = dir.Close()
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func invocationUsage(dir *os.File) (int64, error) {
	entries, err := names(dir)
	if err != nil {
		return 0, err
	}
	var actual int64
	active := false
	for _, name := range entries {
		f, err := openObject(dir, name, unix.O_RDONLY)
		if err != nil {
			return 0, err
		}
		if name == "reservation" {
			b, readErr := io.ReadAll(io.LimitReader(f, 32))
			if readErr != nil || string(b) != "33554432\n" {
				_ = f.Close()
				return 0, tool.ArtifactReadCorrupt
			}
			err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
			active = errors.Is(err, unix.EWOULDBLOCK)
			if err != nil && !active {
				_ = f.Close()
				return 0, err
			}
		} else {
			info, e := f.Stat()
			if e != nil {
				_ = f.Close()
				return 0, e
			}
			actual += info.Size()
		}
		_ = f.Close()
	}
	if active {
		actual = max(actual, tool.ArtifactInvocationBytes)
	}
	return actual, nil
}

// Read 只消费上层已授权的描述符，并验证同一 FD 的完整内容后返回页。
func (s *Store) Read(ctx context.Context, d tool.ArtifactDescriptor, input tool.ReadArtifactInput) (tool.ArtifactPage, error) {
	if ctx == nil || d.Validate() != nil || d.ThreadID() != s.threadID || input.Validate() != nil || d.ID() != input.ID() {
		return tool.ArtifactPage{}, tool.ArtifactReadUnauthorized
	}
	if err := ctx.Err(); err != nil {
		return tool.ArtifactPage{}, err
	}
	dir, err := openChildDirectory(s.thread, string(d.InvocationID()), false)
	if err != nil {
		return tool.ArtifactPage{}, tool.ArtifactReadUnavailable
	}
	defer func() { _ = dir.Close() }()
	f, err := openObject(dir, string(d.ID()), unix.O_RDONLY)
	if err != nil {
		return tool.ArtifactPage{}, tool.ArtifactReadUnavailable
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Size() != d.SavedBytes() {
		return tool.ArtifactPage{}, tool.ArtifactReadCorrupt
	}
	if input.Offset() > uint64(info.Size()) {
		return tool.ArtifactPage{}, tool.ArtifactReadInvalidRange
	}
	start := input.Offset() - min(input.Offset(), 3)
	end := min(uint64(info.Size()), input.Offset()+input.Limit()+3)
	window := make([]byte, 0, end-start)
	hash := sha256.New()
	buffer := make([]byte, 32<<10)
	position := uint64(0)
	for {
		if ctx.Err() != nil {
			return tool.ArtifactPage{}, ctx.Err()
		}
		n, e := f.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
			lo, hi := max(position, start), min(position+uint64(n), end)
			if lo < hi {
				window = append(window, buffer[lo-position:hi-position]...)
			}
			position += uint64(n)
			if position > uint64(d.SavedBytes()) {
				return tool.ArtifactPage{}, tool.ArtifactReadCorrupt
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return tool.ArtifactPage{}, tool.ArtifactReadUnavailable
		}
	}
	if position != uint64(d.SavedBytes()) || hex.EncodeToString(hash.Sum(nil)) != d.Digest() {
		return tool.ArtifactPage{}, tool.ArtifactReadCorrupt
	}
	return tool.RenderArtifactRange(d, input, start, window)
}

func randomID() (tool.ArtifactID, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return tool.ParseArtifactID(hex.EncodeToString(b[:]))
}

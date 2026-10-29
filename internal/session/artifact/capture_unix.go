//go:build darwin || linux

package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"os"

	"easycode/internal/domain"
	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

type captureFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}
type capturedStream struct {
	file            captureFile
	id              tool.ArtifactID
	hash            hash.Hash
	observed, saved int64
	failure         string
	finished        bool
}

// Capture 独占一次命令的保存资源；每个流只允许一个 writer，Finish 必须在两者退出后执行。
type Capture struct {
	directory, reservation *os.File
	thread                 domain.ThreadID
	invocation             tool.InvocationID
	stdout, stderr         capturedStream
	closed                 bool
	syncDirectory          func(*os.File) error
	publish                func(*os.File, string, string) error
}

func (c *Capture) openStreams() error {
	for _, s := range []*capturedStream{&c.stdout, &c.stderr} {
		id, err := randomID()
		if err != nil {
			return err
		}
		s.id = id
		s.hash = sha256.New()
		f, err := openObject(c.directory, string(id)+".stage", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
		if err != nil {
			return err
		}
		s.file = f
	}
	return nil
}

// WriteStdout 排空观察到的输出；磁盘故障只改变保存状态，不传播给管道 reader。
func (c *Capture) WriteStdout(data []byte) (int, error) { return c.stdout.write(data) }

// WriteStderr 排空观察到的错误流，状态与 stdout 独立。
func (c *Capture) WriteStderr(data []byte) (int, error) { return c.stderr.write(data) }

func (s *capturedStream) write(data []byte) (int, error) {
	n := len(data)
	s.observed += int64(n)
	if s.finished {
		return 0, errors.New("artifact capture is finished")
	}
	if s.failure != "" {
		return n, nil
	}
	limit := min(int64(len(data)), tool.ArtifactStreamBytes-s.saved)
	if limit > 0 {
		written, err := s.file.Write(data[:limit])
		if written < 0 || int64(written) > limit {
			s.failure = "write_failed"
			return n, nil
		}
		_, _ = s.hash.Write(data[:written])
		s.saved += int64(written)
		if err != nil || int64(written) != limit {
			s.failure = "write_failed"
		}
	}
	if limit < int64(n) && s.failure == "" {
		s.failure = "stream_limit"
	}
	return n, nil
}

// Finish 同步并发布需要保存的非空前缀；返回值尚未授予读取权限。
func (c *Capture) Finish(stdoutEOF, stderrEOF, needed bool) (tool.ArtifactSaving, tool.ArtifactSaving) {
	return c.finishStream(&c.stdout, tool.ArtifactStdout, stdoutEOF, needed), c.finishStream(&c.stderr, tool.ArtifactStderr, stderrEOF, needed)
}

func (c *Capture) finishStream(s *capturedStream, stream tool.ArtifactStream, eof, needed bool) tool.ArtifactSaving {
	if s.finished {
		return savingFailure("storage_unavailable")
	}
	s.finished = true
	if (!needed && eof) || (s.observed == 0 && eof) {
		if s.file != nil {
			_ = s.file.Close()
			s.file = nil
		}
		_ = unix.Unlinkat(int(c.directory.Fd()), string(s.id)+".stage", 0)
		v, _ := tool.NewArtifactSaving(tool.ArtifactNotNeeded, "", tool.ArtifactDescriptor{})
		return v
	}
	if s.file == nil || s.saved == 0 {
		return savingFailure("storage_unavailable")
	}
	if err := s.file.Sync(); err != nil {
		return savingFailure("sync_failed")
	}
	if err := c.publish(c.directory, string(s.id)+".stage", string(s.id)); err != nil {
		return savingFailure("publish_failed")
	}
	if err := c.syncDirectory(c.directory); err != nil {
		return savingFailure("sync_failed")
	}
	complete := eof && s.saved == s.observed && s.failure == ""
	d, err := tool.NewArtifactDescriptor(s.id, c.thread, c.invocation, stream, s.saved, s.observed, hex.EncodeToString(s.hash.Sum(nil)), complete)
	if err != nil {
		return savingFailure("storage_unavailable")
	}
	status, reason := tool.ArtifactComplete, ""
	if !complete {
		status = tool.ArtifactPartial
		reason = s.failure
		if reason == "" {
			reason = "output_incomplete"
		}
	}
	v, err := tool.NewArtifactSaving(status, reason, d)
	if err != nil {
		return savingFailure("storage_unavailable")
	}
	return v
}

func savingFailure(reason string) tool.ArtifactSaving {
	v, _ := tool.NewArtifactSaving(tool.ArtifactUnavailable, reason, tool.ArtifactDescriptor{})
	return v
}

// Close 在 reader 与 Finish 完成后关闭文件，最后释放 reservation owner。
func (c *Capture) Close() error {
	if c == nil || c.closed {
		return nil
	}
	c.closed = true
	var result error
	for _, s := range []*capturedStream{&c.stdout, &c.stderr} {
		if s.file != nil {
			result = errors.Join(result, s.file.Close())
			s.file = nil
		}
	}
	if c.directory != nil {
		result = errors.Join(result, c.directory.Close())
		c.directory = nil
	}
	if c.reservation != nil {
		result = errors.Join(result, c.reservation.Close())
		c.reservation = nil
	}
	return result
}

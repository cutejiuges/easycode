// Package localfile 提供本地扩展共用的描述符安全打开与有界读取边界。
package localfile

import (
	"context"
	"io"
	"os"
)

// Code 是不包含路径或文件正文的安全读取分类。
type Code string

const (
	Unsafe        Code = "unsafe_local_file"
	LimitExceeded Code = "local_file_limit"
	Changed       Code = "local_file_changed"
	Unsupported   Code = "unsupported_platform"
	ReadFailed    Code = "local_file_read_failed"
)

func (c Code) Error() string { return string(c) }

// Options 声明每个消费者的权限策略；回调只用于显式故障注入，不在锁内调用。
type Options struct {
	RejectWritableFrom string
	Consistent         bool
	BeforeOpen         func(string)
	AfterRead          func()
}

// Read 显式打开一次常规文件并从同一描述符检查与读取。
func Read(ctx context.Context, p string, limit int, options Options) ([]byte, error) {
	if ctx == nil || limit < 1 {
		return nil, Unsafe
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := Open(ctx, p, false, options)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content, err := ReadOpened(ctx, f, limit, options)
	return content.Bytes, err
}

// ReadOpened 从调用方持有的同一描述符读取；资源仍由调用方关闭。
func ReadOpened(ctx context.Context, f *os.File, limit int, options Options) (Content, error) {
	if ctx == nil || f == nil || limit < 1 {
		return Content{}, Unsafe
	}
	if err := ctx.Err(); err != nil {
		return Content{}, err
	}
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return Content{}, Unsafe
	}
	if before.Size() > int64(limit) {
		return Content{}, LimitExceeded
	}
	identity, err := FileIdentity(f)
	if err != nil {
		return Content{}, err
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	content := Content{Identity: identity, ReadBytes: len(b)}
	if err != nil {
		return content, ReadFailed
	}
	if options.AfterRead != nil {
		options.AfterRead()
	}
	if err := ctx.Err(); err != nil {
		return content, err
	}
	if len(b) > limit {
		return content, LimitExceeded
	}
	if options.Consistent {
		after, err := f.Stat()
		if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || int64(len(b)) != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
			return content, Changed
		}
	}
	content.Bytes = b
	return content, nil
}

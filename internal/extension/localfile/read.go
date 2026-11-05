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
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return nil, Unsafe
	}
	if before.Size() > int64(limit) {
		return nil, LimitExceeded
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, ReadFailed
	}
	if options.AfterRead != nil {
		options.AfterRead()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(b) > limit {
		return nil, LimitExceeded
	}
	if options.Consistent {
		after, err := f.Stat()
		if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || int64(len(b)) != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
			return nil, Changed
		}
	}
	return b, nil
}

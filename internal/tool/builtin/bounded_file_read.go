package builtin

import (
	"context"
	"errors"
	"io"
)

// copyBoundedFile 将读取量限制到预算本身，取消或失败仍返回已经消费的字节数。
// sink 由调用方持有；工具保留 bytes，附件保留单个不可变字符串缓冲。
func copyBoundedFile(ctx context.Context, reader io.Reader, sink io.Writer, limit int) (int, error) {
	buffer := make([]byte, min(64<<10, limit))
	consumed, emptyReads := 0, 0
	for consumed < limit {
		if err := ctx.Err(); err != nil {
			return consumed, err
		}
		count, err := reader.Read(buffer[:min(len(buffer), limit-consumed)])
		if count < 0 || count > len(buffer) {
			return consumed, io.ErrShortBuffer
		}
		if count > 0 {
			consumed += count
			emptyReads = 0
			written, writeErr := sink.Write(buffer[:count])
			if writeErr != nil {
				return consumed, writeErr
			}
			if written != count {
				return consumed, io.ErrShortWrite
			}
		} else {
			emptyReads++
			if emptyReads >= 100 {
				return consumed, io.ErrNoProgress
			}
		}
		if errors.Is(err, io.EOF) {
			return consumed, nil
		}
		if err != nil {
			return consumed, err
		}
	}
	return consumed, ctx.Err()
}

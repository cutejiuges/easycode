package builtin

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"easycode/internal/domain"
)

// ReadWorkspaceFile 读取同一已验证 FD 的稳定 UTF-8 快照，不产生工具账本或 Read 资格。
// consumed 在失败时仍报告真实读取量，调用方必须计入整条输入或发现额度。
func (workspace *Workspace) ReadWorkspaceFile(ctx context.Context, path string, limit int) (content string, consumed int, resultErr error) {
	if ctx == nil {
		return "", 0, domain.NewFileFailure(domain.FileInvalid)
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	ref, err := domain.NewFileReference(path, domain.NewWholeFileSelection())
	if err != nil {
		return "", 0, err
	}
	if ref.Path() != path || limit < 0 || limit > domain.MaxFileSourceBytes || workspace == nil || workspace.handle == nil {
		return "", 0, domain.NewFileFailure(domain.FileInvalid)
	}
	file, err := workspace.handle.openFile(path, workspace.hooks)
	if err != nil {
		kind := domain.FileUnavailable
		switch {
		case errors.Is(err, errWorkspaceUnsupported):
			kind = domain.FilePlatformUnsupported
		case errors.Is(err, errTargetNotRegular):
			kind = domain.FileUnsupported
		case errors.Is(err, errTargetTooLarge):
			kind = domain.FileLimit
		}
		return "", 0, domain.NewFileFailure(kind)
	}
	return workspace.readOpenedContent(ctx, file, file.Close, limit)
}

func (workspace *Workspace) readOpenedContent(ctx context.Context, file *os.File, closeHandle func() error, limit int) (content string, consumed int, resultErr error) {
	var closing atomic.Bool
	closed := make(chan struct{})
	closeFile := func() {
		if closing.CompareAndSwap(false, true) {
			_ = closeHandle()
			close(closed)
		} else {
			<-closed
		}
	}
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(callbackDone); closeFile() })
	defer func() {
		if !stop() {
			<-callbackDone
		}
		closeFile()
	}()
	snapshot := readFileSnapshot
	if workspace.hooks.fileSnapshot != nil {
		snapshot = workspace.hooks.fileSnapshot
	}
	before, err := snapshot(file)
	if ctx.Err() != nil {
		return "", 0, ctx.Err()
	}
	if err != nil || before.mode&0o444 == 0 {
		return "", 0, domain.NewFileFailure(domain.FileUnavailable)
	}
	if before.size < 0 || before.size > int64(limit) {
		return "", 0, domain.NewFileFailure(domain.FileLimit)
	}
	var data strings.Builder
	data.Grow(int(before.size))
	consumed, err = copyBoundedFile(ctx, file, &data, limit)
	if err != nil {
		if ctx.Err() != nil {
			return "", consumed, ctx.Err()
		}
		return "", consumed, domain.NewFileFailure(domain.FileUnavailable)
	}
	if workspace.hooks.afterFileRead != nil {
		workspace.hooks.afterFileRead(file)
	}
	if err := ctx.Err(); err != nil {
		return "", consumed, err
	}
	after, err := snapshot(file)
	if ctx.Err() != nil {
		return "", consumed, ctx.Err()
	}
	if err != nil || before != after || after.size != int64(consumed) {
		return "", consumed, domain.NewFileFailure(domain.FileChanged)
	}
	content = data.String()
	if !utf8.ValidString(content) || strings.IndexByte(content, 0) >= 0 {
		return "", consumed, domain.NewFileFailure(domain.FileEncoding)
	}
	return content, consumed, nil
}

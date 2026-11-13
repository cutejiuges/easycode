package plugin

import (
	"context"
	"easycode/internal/extension"
	"easycode/internal/extension/localfile"
	"errors"
	"io"
	"os"
	"sync"
)

type ownedFile struct {
	file    *os.File
	force   bool
	mu      sync.Mutex
	started bool
	done    chan struct{}
	result  error
	before  func()
}

func (f *ownedFile) close() error {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		<-f.done
		return f.result
	}
	f.started = true
	f.mu.Unlock()
	if f.before != nil {
		f.before()
	}
	if f.file.Close() != nil {
		f.result = extension.SourceError("skill_source_close_failed")
	}
	close(f.done)
	return f.result
}

type sourceHandle struct {
	owner        *Sources
	mu           sync.Mutex
	files        []*ownedFile
	stopped      bool
	cancelled    chan struct{}
	closeStarted bool
	closeDone    chan struct{}
	result       error
	directory    *os.File
	identity     localfile.Identity
}

func (h *sourceHandle) stop() {
	h.mu.Lock()
	if !h.stopped {
		h.stopped = true
		close(h.cancelled)
	}
	h.mu.Unlock()
}
func (h *sourceHandle) check(ctx context.Context) error {
	if ctx == nil {
		return extension.SourceError("skill_unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-h.cancelled:
		return context.Canceled
	default:
		return nil
	}
}
func (h *sourceHandle) track(file *os.File, force bool) (*ownedFile, error) {
	owned := &ownedFile{file: file, force: force, before: h.owner.options.BeforeClose, done: make(chan struct{})}
	h.mu.Lock()
	stopped := h.stopped
	if !stopped {
		h.files = append(h.files, owned)
	}
	h.mu.Unlock()
	if stopped {
		_ = owned.close()
		return nil, context.Canceled
	}
	return owned, nil
}
func (h *sourceHandle) forceClose() error {
	h.mu.Lock()
	files := append([]*ownedFile(nil), h.files...)
	h.mu.Unlock()
	var err error
	for _, f := range files {
		// 原目录在读取栈退出前保持打开，避免 openat 使用被复用的描述符编号。
		if f.force {
			err = errors.Join(err, f.close())
		}
	}
	return err
}
func (h *sourceHandle) Close() error {
	h.mu.Lock()
	if h.closeStarted {
		h.mu.Unlock()
		<-h.closeDone
		return h.result
	}
	h.closeStarted = true
	cancelled := h.stopped
	if !h.stopped {
		h.stopped = true
		close(h.cancelled)
	}
	files := append([]*ownedFile(nil), h.files...)
	h.mu.Unlock()
	var result error
	if cancelled {
		result = context.Canceled
	}
	for i := len(files) - 1; i >= 0; i-- {
		result = errors.Join(result, files[i].close())
	}
	if h.owner.release(h) {
		result = errors.Join(result, context.Canceled)
	}
	h.result = result
	close(h.closeDone)
	return h.result
}
func (h *sourceHandle) Identity() localfile.Identity { return h.identity }
func (h *sourceHandle) Read(ctx context.Context, relative string, limit int) (localfile.Content, error) {
	if err := h.check(ctx); err != nil {
		return localfile.Content{}, err
	}
	f, err := localfile.OpenRelative(ctx, h.directory, relative, false, localfile.Options{BeforeOpen: h.owner.options.BeforeOpen})
	if err != nil {
		return localfile.Content{}, err
	}
	owned, err := h.track(f, true)
	if err != nil {
		return localfile.Content{}, err
	}
	defer owned.close()
	content, err := localfile.ReadOpened(ctx, f, limit, localfile.Options{Consistent: true, AfterRead: h.owner.options.AfterRead})
	if cancelErr := h.check(ctx); cancelErr != nil {
		content.Bytes = nil
		return content, cancelErr
	}
	return content, err
}
func (h *sourceHandle) Entries(ctx context.Context, limit int) ([]localfile.Entry, error) {
	if err := h.check(ctx); err != nil {
		return nil, err
	}
	if limit < 0 || limit > 1024 {
		return nil, localfile.LimitExceeded
	}
	names, err := h.directory.Readdirnames(limit + 1)
	if cancelErr := h.check(ctx); cancelErr != nil {
		return nil, cancelErr
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return make([]localfile.Entry, len(names)), localfile.ReadFailed
	}
	if len(names) > limit {
		return make([]localfile.Entry, len(names)), localfile.LimitExceeded
	}
	entries := make([]localfile.Entry, len(names))
	for i, name := range names {
		if err := h.check(ctx); err != nil {
			return nil, err
		}
		entries[i] = localfile.InspectEntry(h.directory, name)
	}
	return entries, nil
}

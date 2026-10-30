//go:build darwin || linux

package artifact

import (
	"context"
	"errors"
	"unicode/utf8"

	"easycode/internal/tool"
)

// TextCapture 持有单一搜索文本流，复用私有发布与预留资源，不具备命令双流接口。
type TextCapture struct{ storage *Capture }

// StartText 在 started 事实之后预留单流额度；来源必须是已实现的搜索能力。
func (s *Store) StartText(ctx context.Context, id tool.InvocationID, capability tool.CapabilityID) (*TextCapture, error) {
	if !tool.ArtifactSearchResult.SupportsCapability(capability) {
		return nil, tool.ArtifactReadUnauthorized
	}
	c, err := s.start(ctx, id, searchReservation())
	if err != nil {
		return nil, err
	}
	return &TextCapture{storage: c}, nil
}

// WriteText 保持 UTF-8 连续前缀；达到上限后继续计数但不再保存。
func (c *TextCapture) WriteText(text string) error {
	if c == nil || c.storage == nil || c.storage.closed || c.storage.stdout.finished {
		return errors.New("artifact capture is finished")
	}
	if !utf8.ValidString(text) {
		return errors.New("invalid artifact text")
	}
	s := &c.storage.stdout
	if s.failure != "" {
		s.observed += int64(len(text))
		return nil
	}
	limit := min(len(text), int(tool.ArtifactStreamBytes-s.saved))
	for limit > 0 && limit < len(text) && !utf8.RuneStart(text[limit]) {
		limit--
	}
	s.observed += int64(len(text))
	written, err := s.file.Write([]byte(text[:limit]))
	if written < 0 || written > limit {
		s.failure = "write_failed"
		return nil
	}
	retained := written
	for retained > 0 && !utf8.ValidString(text[:retained]) {
		retained--
	}
	if retained != written {
		if truncateErr := s.file.Truncate(s.saved + int64(retained)); truncateErr != nil {
			s.saved = 0
			s.failure = "write_failed"
			return nil
		}
	}
	_, _ = s.hash.Write([]byte(text[:retained]))
	s.saved += int64(retained)
	if err != nil || written != limit {
		s.failure = "write_failed"
	} else if limit < len(text) {
		s.failure = "stream_limit"
	}
	return nil
}

// Finish 发布搜索原始文本；完整性只描述所选结果的保存，不代表查询覆盖全部工作区。
func (c *TextCapture) Finish(complete, needed bool) tool.ArtifactSaving {
	if c == nil || c.storage == nil || c.storage.closed {
		return savingFailure("storage_unavailable")
	}
	return c.storage.finishStream(&c.storage.stdout, tool.ArtifactSearchResult, complete, needed)
}

// Close 在文本生产者与发布结束后释放唯一预留。
func (c *TextCapture) Close() error {
	if c == nil || c.storage == nil {
		return nil
	}
	return c.storage.Close()
}

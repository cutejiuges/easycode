package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// ThreadForkedPayload 绑定源的最后提交、完整前缀和子线程的初始标题。
type ThreadForkedPayload struct {
	SourceSequence uint64              `json:"source_seq"`
	SourceSHA256   string              `json:"source_sha256"`
	Title          domain.SessionTitle `json:"title"`
}

// BranchMetadata 描述已验证的当前线程创建事实，不读取父线程。
type BranchMetadata struct {
	ParentThreadID domain.ThreadID
	CreatedAt      time.Time
	SourceSequence uint64
}

// NewThreadForkedDraft 创建子线程的单条归属转换事实草稿。
func NewThreadForkedDraft(parent domain.ThreadID, payload ThreadForkedPayload) (RecordDraft, error) {
	if !parent.Valid() {
		return RecordDraft{}, fmt.Errorf("thread fork parent identity is invalid")
	}
	if err := payload.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventThreadForked, parent, "", payload)
}

// Validate 校验切点、规范前缀摘要和初始标题。
func (payload ThreadForkedPayload) Validate() error {
	if payload.SourceSequence == 0 || payload.Title.Validate() != nil || len(payload.SourceSHA256) != sha256.Size*2 ||
		strings.ToLower(payload.SourceSHA256) != payload.SourceSHA256 {
		return fmt.Errorf("thread fork payload is invalid")
	}
	if _, err := hex.DecodeString(payload.SourceSHA256); err != nil {
		return fmt.Errorf("thread fork source digest is invalid")
	}
	return nil
}

// DecodeThreadForkedPayload 校验当前分支事实的外层和完整必填字段。
func DecodeThreadForkedPayload(record Record) (ThreadForkedPayload, error) {
	if record.EventKind != EventThreadForked || validateThreadForkedEnvelope(record) != nil {
		return ThreadForkedPayload{}, fmt.Errorf("thread fork record envelope is invalid")
	}
	payload, err := decodeThreadForkedPayload(record.Payload)
	if err != nil || payload.SourceSequence != record.Sequence-1 {
		return ThreadForkedPayload{}, fmt.Errorf("thread fork source sequence or payload is invalid")
	}
	return payload, nil
}

func decodeThreadForkedPayload(data []byte) (ThreadForkedPayload, error) {
	var payload ThreadForkedPayload
	if err := codec.DecodeObject(data, &payload, "source_seq", "source_sha256", "title"); err != nil {
		return ThreadForkedPayload{}, fmt.Errorf("thread fork payload is invalid")
	}
	return payload, payload.Validate()
}

func validateThreadForkedEnvelope(record Record) error {
	if record.Sequence < 2 || record.BatchID != record.Sequence || record.BatchIndex != 0 || record.BatchSize != 1 ||
		record.TurnID != "" || !record.ParentThreadID.Valid() || !record.ThreadID.Valid() || record.ParentThreadID == record.ThreadID {
		return fmt.Errorf("thread fork record envelope is invalid")
	}
	return nil
}

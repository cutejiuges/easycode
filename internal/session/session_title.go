package session

import (
	"errors"

	"easycode/internal/domain"
)

type sessionTitleUpdatedPayload struct {
	Title domain.SessionTitle `json:"title"`
}

// NewSessionTitleUpdatedDraft 创建用于保存规范标题的单条事实草稿。
func NewSessionTitleUpdatedDraft(title domain.SessionTitle) (RecordDraft, error) {
	if err := title.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventSessionTitleUpdated, "", "", sessionTitleUpdatedPayload{Title: title})
}

// DecodeSessionTitleUpdated 严格读取会话标题更新的唯一当前 payload。
func DecodeSessionTitleUpdated(record Record) (domain.SessionTitle, error) {
	payload, err := decodeKnownPayload[sessionTitleUpdatedPayload](record, EventSessionTitleUpdated)
	if err != nil || payload.Title.Validate() != nil {
		return "", errors.New("session title update payload is invalid")
	}
	return payload.Title, nil
}

func validateSessionTitleEnvelope(record Record) error {
	if record.ParentThreadID != "" || record.TurnID != "" {
		return errors.New("session title record cannot contain a turn or parent thread identity")
	}
	if record.BatchSize != 1 || record.BatchID != record.Sequence {
		return errors.New("session title record requires a single-record batch")
	}
	return nil
}

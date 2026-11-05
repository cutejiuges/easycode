package domain

import (
	"crypto/rand"
	"time"
)

// QuestionRequestID 是一次活动问卷的临时关联身份，不是持久化输入身份。
type QuestionRequestID string

func GenerateQuestionRequestID() (QuestionRequestID, error) {
	v, err := generateUUIDv7(time.Now(), rand.Reader)
	return QuestionRequestID(v), err
}
func (id QuestionRequestID) Valid() bool { _, err := parseUUIDv7(string(id)); return err == nil }

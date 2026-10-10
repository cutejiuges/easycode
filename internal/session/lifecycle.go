package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"easycode/internal/domain"
)

// RootJournalConfig 只包含可以进入 Session metadata 的非敏感恢复字段。
type RootJournalConfig struct {
	Provider     domain.ProviderFamily
	ProviderWire string
	Model        string
	CreationCWD  string
	CreatedAt    time.Time
}

// GenerateRootIdentity 生成彼此独立的 UUIDv7 Session 与 root Thread 标识。
func GenerateRootIdentity() (Identity, error) {
	sessionID, err := domain.GenerateSessionID()
	if err != nil {
		return Identity{}, err
	}
	threadID, err := domain.GenerateThreadID()
	if err != nil {
		return Identity{}, err
	}
	return Identity{SessionID: sessionID, ThreadID: threadID}, nil
}

// NormalizeCreationCWD 将创建目录规范化为绝对路径，但不解析 symlink 或改变进程目录。
func NormalizeCreationCWD(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("session creation cwd is required")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve session creation cwd: %w", err)
	}
	return filepath.Clean(absolute), nil
}

// CreateRootJournal 创建 journal 并 durable 写入唯一的初始 metadata batch。
func CreateRootJournal(
	ctx context.Context,
	repository *Repository,
	identity Identity,
	config RootJournalConfig,
) (*JournalWriter, []Record, error) {
	if repository == nil {
		return nil, nil, fmt.Errorf("session repository is required")
	}
	if !identity.SessionID.Valid() || !identity.ThreadID.Valid() {
		return nil, nil, fmt.Errorf("root session identity is invalid")
	}
	if !config.Provider.Valid() || strings.TrimSpace(config.ProviderWire) == "" || strings.TrimSpace(config.Model) == "" {
		return nil, nil, fmt.Errorf("root session provider metadata is invalid")
	}
	creationCWD, err := NormalizeCreationCWD(config.CreationCWD)
	if err != nil {
		return nil, nil, err
	}
	createdAt := config.CreatedAt.UTC()
	if config.CreatedAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	lease, err := repository.Create(ctx, identity.ThreadID)
	if err != nil {
		return nil, nil, err
	}
	writer, err := StartJournalWriter(lease, identity, 1)
	if err != nil {
		return nil, nil, errors.Join(err, lease.Close())
	}
	sessionDraft, err := NewSessionMetaDraft(SessionMetaPayload{
		RootThreadID: identity.ThreadID, CreatedAt: createdAt,
		Provider: config.Provider, ProviderWire: config.ProviderWire,
		Model: config.Model, CreationCWD: creationCWD,
	})
	if err != nil {
		closeErr := writer.Close(context.Background())
		return nil, nil, errors.Join(err, closeErr)
	}
	threadDraft, err := NewThreadMetaDraft(ThreadMetaPayload{Root: true})
	if err != nil {
		closeErr := writer.Close(context.Background())
		return nil, nil, errors.Join(err, closeErr)
	}
	records, err := writer.AppendBatch(ctx, []RecordDraft{sessionDraft, threadDraft})
	if err != nil {
		closeErr := writer.Close(context.Background())
		return nil, nil, errors.Join(err, closeErr)
	}
	return writer, records, nil
}

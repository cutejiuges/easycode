// Package session 定义 append-only Session 事实源和索引边界。
package session

import (
	"context"
	"encoding/json"
	"time"

	"easycode/internal/domain"
)

// Record 是写入 JSONL 事实源的版本化记录。
type Record struct {
	SchemaVersion int              `json:"schema_version"`
	Sequence      uint64           `json:"seq"`
	Timestamp     time.Time        `json:"timestamp"`
	SessionID     domain.SessionID `json:"session_id"`
	ThreadID      domain.ThreadID  `json:"thread_id"`
	TurnID        domain.TurnID    `json:"turn_id,omitempty"`
	Kind          string           `json:"kind"`
	Payload       json.RawMessage  `json:"payload"`
}

// Store 定义 JSONL 事实源最小能力，SQLite 索引实现不能替代它。
type Store interface {
	Append(context.Context, Record) error
	Load(context.Context, domain.ThreadID) ([]Record, error)
	Flush(context.Context, domain.ThreadID) error
}

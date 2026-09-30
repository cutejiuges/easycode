// Package session 实现 append-only JSONL Session 事实源。
package session

import (
	"encoding/json"
	"time"

	"easycode/internal/domain"
)

const (
	// EnvelopeVersion 是当前公共 JSONL 信封版本。
	EnvelopeVersion = 1
	// MaxRecordBytes 限制单条 canonical JSON 记录大小。
	MaxRecordBytes = 16 << 20
	// MaxBatchBytes 限制单个逻辑 batch 的总大小。
	MaxBatchBytes = 64 << 20
)

// ReplayRequirement 声明未知消费者能否安全跳过一条记录。
type ReplayRequirement string

const (
	ReplayRequired ReplayRequirement = "required"
	ReplayOptional ReplayRequirement = "optional"
)

// EventKind 标识 Session 事实记录种类。
type EventKind string

const (
	EventSessionMeta          EventKind = "session_meta"
	EventThreadMeta           EventKind = "thread_meta"
	EventTurnStarted          EventKind = "turn_started"
	EventProviderNativeCommit EventKind = "provider_native_commit"
	EventTurnCompleted        EventKind = "turn_completed"
	EventTurnFailed           EventKind = "turn_failed"
)

// Record 是一行可独立校验的 JSONL v1 信封。
type Record struct {
	SchemaVersion     int               `json:"schema_version"`
	PayloadVersion    int               `json:"payload_version"`
	ReplayRequirement ReplayRequirement `json:"replay_requirement"`
	Sequence          uint64            `json:"seq"`
	Timestamp         time.Time         `json:"timestamp"`
	SessionID         domain.SessionID  `json:"session_id"`
	ThreadID          domain.ThreadID   `json:"thread_id"`
	ParentThreadID    domain.ThreadID   `json:"parent_thread_id,omitempty"`
	TurnID            domain.TurnID     `json:"turn_id,omitempty"`
	EventKind         EventKind         `json:"event_kind"`
	BatchID           uint64            `json:"batch_id"`
	BatchIndex        uint32            `json:"batch_index"`
	BatchSize         uint32            `json:"batch_size"`
	Payload           json.RawMessage   `json:"payload"`
	Checksum          string            `json:"checksum"`
}

// RecordDraft 是尚未由单 writer 分配顺序和 batch 边界的强类型记录。
type RecordDraft struct {
	descriptor     Descriptor
	parentThreadID domain.ThreadID
	turnID         domain.TurnID
	payload        json.RawMessage
}

// EventKind 返回 draft 的已校验事件种类。
func (draft RecordDraft) EventKind() EventKind { return draft.descriptor.Kind }

// ParentThreadID 返回 draft 的父 thread 标识。
func (draft RecordDraft) ParentThreadID() domain.ThreadID { return draft.parentThreadID }

// TurnID 返回 draft 的 turn 标识。
func (draft RecordDraft) TurnID() domain.TurnID { return draft.turnID }

// PayloadBytes 返回 draft 已编码 payload 的独立副本。
func (draft RecordDraft) PayloadBytes() json.RawMessage {
	return append(json.RawMessage(nil), draft.payload...)
}

// Identity 固定一个 thread journal 的不可变身份。
type Identity struct {
	SessionID domain.SessionID
	ThreadID  domain.ThreadID
}

// SessionMetaPayload 保存恢复所需的非敏感根 Session 元数据。
type SessionMetaPayload struct {
	RootThreadID   domain.ThreadID       `json:"root_thread_id"`
	CreatedAt      time.Time             `json:"created_at"`
	Provider       domain.ProviderFamily `json:"provider"`
	ProviderWire   string                `json:"provider_wire"`
	Model          string                `json:"model"`
	SchemaRevision int                   `json:"schema_revision"`
	CreationCWD    string                `json:"creation_cwd"`
}

// ThreadMetaPayload 保存 thread 与父级的稳定关系。
type ThreadMetaPayload struct {
	Root           bool            `json:"root"`
	ParentThreadID domain.ThreadID `json:"parent_thread_id,omitempty"`
}

// TurnStartedPayload 表示当前文本 turn 已 durable 开始。
type TurnStartedPayload struct{}

// NativeCommitPayload 保存 Provider-owned opaque 原生历史增量。
type NativeCommitPayload struct {
	Provider       domain.ProviderFamily `json:"provider"`
	Wire           string                `json:"wire"`
	PayloadVersion int                   `json:"payload_version"`
	Payload        json.RawMessage       `json:"payload"`
}

// TurnCompletedPayload 表示当前文本 turn 已 durable 成功结束。
type TurnCompletedPayload struct{}

// TurnFailedPayload 表示不进入 Provider 原生历史的失败边界。
type TurnFailedPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

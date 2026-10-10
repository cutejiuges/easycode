// Package session 实现 append-only JSONL Session 事实源。
package session

import (
	"encoding/json"
	"time"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

const (
	// EnvelopeVersion 是当前公共 JSONL 信封版本。
	EnvelopeVersion = 1
	// MaxRecordBytes 限制单条 canonical JSON 记录大小。
	MaxRecordBytes = 16 << 20
	// MaxBatchBytes 限制单个逻辑 batch 的总大小。
	MaxBatchBytes = 64 << 20
)

// EventKind 标识 Session 事实记录种类。
type EventKind string

const (
	EventSessionMeta          EventKind = "session_meta"
	EventThreadMeta           EventKind = "thread_meta"
	EventTurnStarted          EventKind = "turn_started"
	EventProviderNativeCommit EventKind = "provider_native_commit"
	EventSampleUsage          EventKind = "sample_usage"
	EventToolCallReady        EventKind = "tool_call_ready"
	EventToolExecutionStarted EventKind = "tool_execution_started"
	EventToolCallResult       EventKind = "tool_call_result"
	EventTurnCompleted        EventKind = "turn_completed"
	EventTurnFailed           EventKind = "turn_failed"
)

// Record 是一行可独立校验的 JSONL v1 信封。
type Record struct {
	SchemaVersion  int              `json:"schema_version"`
	PayloadVersion int              `json:"payload_version"`
	Sequence       uint64           `json:"seq"`
	Timestamp      time.Time        `json:"timestamp"`
	SessionID      domain.SessionID `json:"session_id"`
	ThreadID       domain.ThreadID  `json:"thread_id"`
	ParentThreadID domain.ThreadID  `json:"parent_thread_id,omitempty"`
	TurnID         domain.TurnID    `json:"turn_id,omitempty"`
	EventKind      EventKind        `json:"event_kind"`
	BatchID        uint64           `json:"batch_id"`
	BatchIndex     uint32           `json:"batch_index"`
	BatchSize      uint32           `json:"batch_size"`
	Payload        json.RawMessage  `json:"payload"`
	Checksum       string           `json:"checksum"`
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
	RootThreadID domain.ThreadID       `json:"root_thread_id"`
	CreatedAt    time.Time             `json:"created_at"`
	Provider     domain.ProviderFamily `json:"provider"`
	ProviderWire string                `json:"provider_wire"`
	Model        string                `json:"model"`
	CreationCWD  string                `json:"creation_cwd"`
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

// ReadInputPayload 保存可严格恢复的Read输入。
type ReadInputPayload struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

// GlobInputPayload 保存可严格恢复的 Glob 输入。
type GlobInputPayload struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Limit   int    `json:"limit"`
}

// GrepInputPayload 保存可严格恢复的 Grep 输入。
type GrepInputPayload struct {
	Pattern         string              `json:"pattern"`
	Path            string              `json:"path"`
	Glob            string              `json:"glob"`
	OutputMode      tool.GrepOutputMode `json:"output_mode"`
	CaseInsensitive bool                `json:"case_insensitive"`
	BeforeContext   int                 `json:"before_context"`
	AfterContext    int                 `json:"after_context"`
	Limit           int                 `json:"limit"`
}

// ToolCallReadyPayload 保存执行前已经durable的完整调用事实。
type ToolCallReadyPayload struct {
	InvocationID   tool.InvocationID   `json:"invocation_id"`
	ProviderCallID tool.ProviderCallID `json:"provider_call_id"`
	SampleIndex    uint32              `json:"sample_index"`
	CallIndex      uint32              `json:"call_index"`
	Capability     tool.CapabilityID   `json:"capability"`
	ReadInput      *ReadInputPayload   `json:"read_input,omitempty"`
	GlobInput      *GlobInputPayload   `json:"glob_input,omitempty"`
	GrepInput      *GrepInputPayload   `json:"grep_input,omitempty"`
}

// ToolExecutionStartedPayload 标记executor接收调用的线性化点。
type ToolExecutionStartedPayload struct {
	InvocationID tool.InvocationID `json:"invocation_id"`
}

// ReadResultMetadataPayload 保存不含绝对路径和文件正文的Read结果元数据。
type ReadResultMetadataPayload struct {
	RelativePath      string `json:"relative_path"`
	RequestedOffset   int    `json:"requested_offset"`
	RequestedLimit    int    `json:"requested_limit"`
	StartLine         int    `json:"start_line"`
	EndLine           int    `json:"end_line"`
	ReachedEOF        bool   `json:"reached_eof"`
	LongLineTruncated bool   `json:"long_line_truncated"`
	OutputTruncated   bool   `json:"output_truncated"`
}

type SearchSkipCountsPayload struct {
	Binary      int `json:"binary"`
	InvalidUTF8 int `json:"invalid_utf8"`
	TooLarge    int `json:"too_large"`
	Unreadable  int `json:"unreadable"`
	Unsupported int `json:"unsupported"`
	Disappeared int `json:"disappeared"`
}

type GlobResultMetadataPayload struct {
	Matches          []string                    `json:"matches"`
	Truncated        bool                        `json:"truncated"`
	OmittedMatches   int                         `json:"omitted_matches"`
	VisitedEntries   int                         `json:"visited_entries"`
	IncompleteReason tool.SearchIncompleteReason `json:"incomplete_reason"`
	Skipped          SearchSkipCountsPayload     `json:"skipped"`
}

type GrepMatchPayload struct {
	RelativePath string `json:"relative_path"`
	Line         int    `json:"line"`
	Text         string `json:"text"`
	MatchingLine bool   `json:"matching_line"`
	Count        int    `json:"count"`
}

type GrepResultMetadataPayload struct {
	Mode             tool.GrepOutputMode         `json:"mode"`
	Matches          []GrepMatchPayload          `json:"matches"`
	MatchingLines    int                         `json:"matching_lines"`
	Truncated        bool                        `json:"truncated"`
	OmittedMatches   int                         `json:"omitted_matches"`
	VisitedEntries   int                         `json:"visited_entries"`
	ScannedFiles     int                         `json:"scanned_files"`
	ScannedBytes     int64                       `json:"scanned_bytes"`
	IncompleteReason tool.SearchIncompleteReason `json:"incomplete_reason"`
	Skipped          SearchSkipCountsPayload     `json:"skipped"`
}

// ToolCallResultPayload 保存无需重新执行即可恢复的冻结结果。
type ToolCallResultPayload struct {
	InvocationID tool.InvocationID          `json:"invocation_id"`
	Capability   tool.CapabilityID          `json:"capability"`
	Status       tool.ResultStatus          `json:"status"`
	Code         string                     `json:"code"`
	Preview      string                     `json:"preview"`
	ReadMetadata *ReadResultMetadataPayload `json:"read_metadata,omitempty"`
	GlobMetadata *GlobResultMetadataPayload `json:"glob_metadata,omitempty"`
	GrepMetadata *GrepResultMetadataPayload `json:"grep_metadata,omitempty"`
}

// TurnCompletedPayload 表示当前文本 turn 已 durable 成功结束。
type TurnCompletedPayload struct{}

// TurnFailedPayload 表示不进入 Provider 原生历史的失败边界。
type TurnFailedPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

type recordPayload interface {
	SessionMetaPayload | ThreadMetaPayload | TurnStartedPayload | NativeCommitPayload |
		SampleUsagePayload | ToolCallReadyPayload | ToolExecutionStartedPayload |
		ToolCallResultPayload | TurnCompletedPayload | TurnFailedPayload
}

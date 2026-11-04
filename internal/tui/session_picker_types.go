package tui

import (
	"easycode/internal/domain"
	"time"
)

// SessionEntry 是选择器允许展示和搜索的元数据。
type SessionEntry struct {
	ThreadID         domain.ThreadID
	UpdatedAt        time.Time
	Family           domain.ProviderFamily
	Wire, Model, CWD string
}

// PreviewMessage 保留原始消息身份以及展示截断状态。
type PreviewMessage struct {
	TurnID    domain.TurnID
	Message   domain.SemanticMessage
	Truncated bool
}

// SessionPreview 仅供当前预览展示，不是恢复事实源。
type SessionPreview struct {
	Messages              []PreviewMessage
	Incomplete, Truncated bool
}

// PickerOperation 区分实际存在的启动操作。
type PickerOperation uint8

const (
	PickerList PickerOperation = iota + 1
	PickerRefresh
	PickerPreview
	PickerResume
)

// PickerRequest 将请求身份、范围和目标绑定在一起。
type PickerRequest struct {
	ID        uint64
	Operation PickerOperation
	All       bool
	ThreadID  domain.ThreadID
}

// PickerResult 是宿主已完成操作的投影，不携带存储句柄。
type PickerResult struct {
	Request PickerRequest
	Entries []SessionEntry
	HasMore bool
	Warning string
	Preview SessionPreview
	Chat    *Model
	Error   string
	Fatal   bool
}

// PickerHost 只向界面提供操作接收、结果和资源接收确认。
type PickerHost interface {
	Submit(PickerRequest) bool
	Results() <-chan PickerResult
	CancelPending()
	Accept(uint64) bool
	Stop()
}

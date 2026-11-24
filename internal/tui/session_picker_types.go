package tui

import (
	"time"

	"easycode/internal/domain"
)

// SessionEntry 是选择器允许展示和搜索的元数据。
type SessionEntry struct {
	ThreadID         domain.ThreadID
	ParentThreadID   domain.ThreadID
	UpdatedAt        time.Time
	Title            domain.SessionTitle
	Family           domain.ProviderFamily
	Wire, Model, CWD string
}

// PreviewMessage 保留原始消息身份以及展示截断状态。
type PreviewMessage struct {
	Files     domain.FileSummary
	TurnID    domain.TurnID
	Message   domain.SemanticMessage
	Truncated bool
}

// SessionPreview 仅供当前预览展示，不是恢复事实源。
type SessionPreview struct {
	Title                 domain.SessionTitle
	ParentThreadID        domain.ThreadID
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
	PickerOpen
	PickerBranch
)

// PickerRequest 将请求身份、范围和目标绑定在一起。
type PickerRequest struct {
	ID        uint64
	Operation PickerOperation
	All       bool
	Source    domain.ThreadID
	ThreadID  domain.ThreadID
	Title     domain.SessionTitle
}

// PickerStage 区分查询结果、待接管候选和宿主完成后的交付。
type PickerStage uint8

const (
	PickerCompleted PickerStage = iota
	PickerPrepared
	PickerActivated
	PickerCancelled
)

// BranchPublication 区分本地分支创建的持久化结果。
type BranchPublication uint8

const (
	BranchNotPublished BranchPublication = iota
	BranchPublished
	BranchPublicationUncertain
)

// PickerResult 是宿主已完成操作的投影，不携带存储句柄。
type PickerResult struct {
	Request           PickerRequest
	Stage             PickerStage
	Entries           []SessionEntry
	HasMore           bool
	Warning           string
	Preview           SessionPreview
	Chat              *Model
	Error             string
	Fatal             bool
	DirectoryMismatch bool
	BranchThreadID    domain.ThreadID
	BranchPublication BranchPublication
}

// PickerHost 只向界面提供操作接收、结果和资源接收确认。
type PickerHost interface {
	Submit(PickerRequest) bool
	Results() <-chan PickerResult
	CancelPending()
	Accept(uint64) bool
	Stop()
}

// InteractivePickerHost 额外负责取消聊天内切换并确认候选与许可的清理。
type InteractivePickerHost interface {
	PickerHost
	CancelResume(PickerRequest)
}

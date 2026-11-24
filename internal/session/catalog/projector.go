package catalog

import (
	"fmt"

	"easycode/internal/session"
)

// Projector 将完整 Loader 与 ReplayPlanner 结果投影为不可变 Entry。
type Projector struct{}

// NewProjector 创建纯内存投影器。
func NewProjector() *Projector { return &Projector{} }

// Project 只使用已提交 records 和已验证的元数据构造 Catalog Entry。
func (projector *Projector) Project(relativePath string, loaded session.LoadResult, plan session.ReplayMetadata) (Entry, error) {
	if projector == nil {
		return Entry{}, fmt.Errorf("catalog projector is required")
	}
	if len(loaded.Records) == 0 {
		return Entry{}, fmt.Errorf("catalog projection requires committed records")
	}
	if loaded.Identity != plan.Identity || !plan.Identity.SessionID.Valid() || !plan.Identity.ThreadID.Valid() {
		return Entry{}, fmt.Errorf("catalog projection identity is invalid")
	}
	last := loaded.Records[len(loaded.Records)-1]
	if last.SessionID != plan.Identity.SessionID || last.ThreadID != plan.Identity.ThreadID ||
		last.Sequence+1 != loaded.NextSequence {
		return Entry{}, fmt.Errorf("catalog projection tail is inconsistent")
	}
	entry := Entry{
		SessionID: plan.Identity.SessionID, ThreadID: plan.Identity.ThreadID,
		JournalPath: relativePath, CreatedAt: plan.SessionMetadata.CreatedAt,
		UpdatedAt: last.Timestamp, LastSequence: last.Sequence, LastChecksum: last.Checksum,
		CreationCWD:    plan.SessionMetadata.CreationCWD,
		ProviderFamily: plan.SessionMetadata.Provider, ProviderWire: plan.SessionMetadata.ProviderWire,
		Model: plan.SessionMetadata.Model, Title: plan.Title,
	}
	if plan.Branch != nil {
		entry.ParentThreadID = parentThreadID(plan.Branch.ParentThreadID)
		entry.CreatedAt = plan.Branch.CreatedAt
	}
	if err := validateEntry(entry); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

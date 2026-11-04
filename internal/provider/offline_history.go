package provider

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
)

// HistorySource 描述已存储历史的身份，不携带连接信息。
type HistorySource struct {
	ThreadID domain.ThreadID
	Family   domain.ProviderFamily
	Wire     string
	Model    string
}

// Validate 检查离线投影来源的完整性。
func (source HistorySource) Validate() error {
	if !source.ThreadID.Valid() || !source.Family.Valid() || strings.TrimSpace(source.Wire) == "" || strings.TrimSpace(source.Model) == "" || !utf8.ValidString(source.Model) {
		return fmt.Errorf("history source is invalid")
	}
	return nil
}

// StoredHistoryReader 将原生记录单向投影为可见文本，并独立校验用量证据。
type StoredHistoryReader interface {
	ProjectStoredHistory(context.Context, HistorySource, []RestoreRecord) (domain.SemanticHistoryView, error)
	CompactionUsageValidator
}

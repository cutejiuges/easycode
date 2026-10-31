package domain

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// CompactionID 是独立于普通 turn 的压缩操作身份。
type CompactionID string

const MaxContextSummaryBytes = 256 << 10

// GenerateCompactionID 显式分配压缩操作身份。
func GenerateCompactionID() (CompactionID, error) {
	value, err := generateUUIDv7(time.Now(), rand.Reader)
	return CompactionID(value), err
}

// ParseCompactionID 拒绝非 canonical UUIDv7 身份。
func ParseCompactionID(value string) (CompactionID, error) {
	if _, err := parseUUIDv7(value); err != nil {
		return "", fmt.Errorf("invalid compaction ID: %w", err)
	}
	return CompactionID(value), nil
}

// Valid 校验压缩操作身份。
func (id CompactionID) Valid() bool {
	_, err := ParseCompactionID(string(id))
	return err == nil
}

// ContextSummary 是有效窗口中的摘要，不具有输入、授权或 turn 身份。
type ContextSummary struct{ text string }

// NewContextSummary 校验摘要正文并保留原始文本。
func NewContextSummary(text string) (ContextSummary, error) {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || len(text) > MaxContextSummaryBytes {
		return ContextSummary{}, fmt.Errorf("context summary is invalid")
	}
	return ContextSummary{text: text}, nil
}

// Text 返回不可变摘要正文。
func (summary ContextSummary) Text() string { return summary.text }

// ActiveContextView 是用于规划和展示的单向有效窗口投影。
// 原生请求必须由 Provider 自有窗口编译，不能从本视图反向重建。
type ActiveContextView struct {
	history  SemanticHistoryView
	summary  ContextSummary
	revision uint64
}

// NewActiveContextView 创建不共享历史切片的窗口快照；零值摘要表示尚无 checkpoint。
func NewActiveContextView(history SemanticHistoryView, summary ContextSummary, revision uint64) (ActiveContextView, error) {
	if !history.Provider.Valid() || history.Turns == nil {
		return ActiveContextView{}, fmt.Errorf("active context history is invalid")
	}
	for _, turn := range history.Turns {
		if err := turn.Validate(); err != nil {
			return ActiveContextView{}, err
		}
	}
	if summary.text != "" {
		if _, err := NewContextSummary(summary.text); err != nil {
			return ActiveContextView{}, err
		}
		if revision == 0 {
			return ActiveContextView{}, fmt.Errorf("summary context revision is required")
		}
	}
	return ActiveContextView{history: history.Clone(), summary: summary, revision: revision}, nil
}

// Validate 拒绝零值窗口和非法摘要。
func (view ActiveContextView) Validate() error {
	_, err := NewActiveContextView(view.history, view.summary, view.revision)
	return err
}

// History 返回摘要之后真实消息的独立副本。
func (view ActiveContextView) History() SemanticHistoryView { return view.history.Clone() }

// Summary 返回独立摘要以及是否存在 checkpoint。
func (view ActiveContextView) Summary() (ContextSummary, bool) {
	return view.summary, view.summary.text != ""
}

// Revision 返回普通原生提交和 checkpoint 共同推进的上下文版本。
func (view ActiveContextView) Revision() uint64 { return view.revision }

// Clone 返回不共享可变切片的窗口快照。
func (view ActiveContextView) Clone() ActiveContextView {
	view.history = view.history.Clone()
	return view
}

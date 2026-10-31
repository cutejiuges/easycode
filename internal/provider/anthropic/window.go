package anthropic

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
	"fmt"
)

// windowState 只在锁内复制不可变 checkpoint 引用及有界元数据。
func (history *nativeHistory) windowState() (*provider.CheckpointEnvelope, int, uint64) {
	history.mu.RLock()
	defer history.mu.RUnlock()
	return history.checkpoint, history.checkpointOffset, history.checkpointCount
}

// windowRequestEntries 只服务原生请求编译，不用于历史投影或提交。
func (conversation *Conversation) windowRequestEntries(entries []nativeHistoryEntry) []nativeHistoryEntry {
	checkpoint, offset, _ := conversation.history.windowState()
	if checkpoint == nil {
		return entries
	}
	if offset > len(entries) {
		return nil
	}
	conversation.history.mu.RLock()
	summary := conversation.history.summaryNative.clone()
	conversation.history.mu.RUnlock()
	result := make([]nativeHistoryEntry, 0, len(entries)-offset+1)
	result = append(result, summary)
	return append(result, entries[offset:]...)
}

func (conversation *Conversation) windowFootprint(entries []nativeHistoryEntry, revision uint64) (domain.NativeHistoryFootprint, error) {
	_, _, count := conversation.history.windowState()
	if revision > ^uint64(0)-count {
		return domain.NativeHistoryFootprint{}, fmt.Errorf("context revision overflow")
	}
	return historyFootprint(conversation.windowRequestEntries(entries), revision+count)
}

func (conversation *Conversation) projectActiveWindow(entries []nativeHistoryEntry, revision uint64) (domain.ActiveContextView, error) {
	checkpoint, offset, count := conversation.history.windowState()
	if offset > len(entries) || revision > ^uint64(0)-count {
		return domain.ActiveContextView{}, fmt.Errorf("context window boundary is invalid")
	}
	var summary domain.ContextSummary
	if checkpoint != nil {
		conversation.history.mu.RLock()
		summary = conversation.history.contextSummary
		conversation.history.mu.RUnlock()
	}
	return domain.NewActiveContextView(projectHistory(entries[offset:]), summary, revision+count)
}

// ProjectActiveContext 返回已提交窗口，完整历史仍由 ProjectHistory 提供。
func (conversation *Conversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	snapshot := conversation.history.snapshotWindow()
	if snapshot.offset > len(snapshot.entries) || snapshot.revision > ^uint64(0)-snapshot.count {
		return domain.ActiveContextView{}, fmt.Errorf("context window boundary is invalid")
	}
	return domain.NewActiveContextView(projectHistory(snapshot.entries[snapshot.offset:]), snapshot.summary, snapshot.revision+snapshot.count)
}

type nativeWindowSnapshot struct {
	entries       []nativeHistoryEntry
	revision      uint64
	checkpoint    *provider.CheckpointEnvelope
	offset        int
	count         uint64
	summary       domain.ContextSummary
	summaryNative nativeHistoryEntry
}

func (history *nativeHistory) snapshotWindow() nativeWindowSnapshot {
	history.mu.RLock()
	defer history.mu.RUnlock()
	entries := make([]nativeHistoryEntry, len(history.entries))
	for i, entry := range history.entries {
		entries[i] = entry.clone()
	}
	return nativeWindowSnapshot{entries: entries, revision: history.revision, checkpoint: history.checkpoint, offset: history.checkpointOffset, count: history.checkpointCount, summary: history.contextSummary, summaryNative: history.summaryNative.clone()}
}

func (snapshot nativeWindowSnapshot) requestEntries() []nativeHistoryEntry {
	if snapshot.checkpoint == nil {
		return snapshot.entries
	}
	entries := make([]nativeHistoryEntry, 0, len(snapshot.entries)-snapshot.offset+1)
	entries = append(entries, snapshot.summaryNative)
	return append(entries, snapshot.entries[snapshot.offset:]...)
}

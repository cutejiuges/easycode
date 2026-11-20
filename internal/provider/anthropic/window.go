package anthropic

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
)

type nativeHistory struct {
	provider.NativeHistory[nativeHistoryEntry]
}

type nativeWindowSnapshot = provider.NativeWindow[nativeHistoryEntry]

func (h *nativeHistory) snapshot() []nativeHistoryEntry {
	entries, _ := h.Entries()
	return entries
}

func (h *nativeHistory) snapshotWindow() nativeWindowSnapshot { return h.Snapshot() }
func (h *nativeHistory) commit(entry nativeHistoryEntry)      { h.Append(entry) }
func (h *nativeHistory) installCheckpoint(checkpoint provider.CheckpointEnvelope, entry nativeHistoryEntry, summary domain.ContextSummary) {
	h.InstallCheckpoint(checkpoint, entry, summary)
}

func (conversation *Conversation) windowRequestEntries(entries []nativeHistoryEntry) []nativeHistoryEntry {
	return conversation.history.WindowFor(entries, 0).RequestEntries()
}

// ProjectActiveContext 返回已提交窗口，完整历史仍由 ProjectHistory 提供。
func (conversation *Conversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	return provider.ProjectNativeWindow(conversation.history.Snapshot(), projectHistory)
}

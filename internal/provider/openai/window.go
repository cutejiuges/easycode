package openai

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
)

type nativeHistory = provider.NativeHistory[nativeHistoryEntry]

type nativeWindowSnapshot = provider.NativeWindow[nativeHistoryEntry]

// ProjectActiveContext 返回已提交窗口，完整历史仍由 ProjectHistory 提供。
func (conversation *Conversation) ProjectActiveContext() (domain.ActiveContextView, error) {
	return provider.ProjectNativeWindow(conversation.history.ActiveSnapshot(), projectHistory)
}

// ProjectContextWindow 返回同一已提交窗口的语义视图和原生体量。
func (conversation *Conversation) ProjectContextWindow() (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	return projectContextWindow(conversation.history.ActiveSnapshot())
}

func projectContextWindow(window nativeWindowSnapshot) (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	return provider.ProjectContextWindow(window, projectHistory, historyFootprint)
}

package tui

type chatFocus uint8

const (
	focusComposer chatFocus = iota
	focusClosed
	focusHandoffNotice
	focusQuestion
	focusHookTrust
	focusApproval
	focusHooks
	focusSkills
	focusCache
	focusContext
	focusPlugins
	focusTasks
	focusTranscript
	focusTitle
	focusPlanReview
	focusRecalledSend
)

func (model Model) focus() chatFocus {
	switch {
	case model.title.closed:
		return focusClosed
	case model.handoffNotice != "":
		return focusHandoffNotice
	case model.question != nil:
		return focusQuestion
	case model.hookTrust != nil:
		return focusHookTrust
	case model.approval != nil:
		return focusApproval
	case model.hooks.open:
		return focusHooks
	case model.skillsOpen:
		return focusSkills
	case model.cachePanel != nil:
		return focusCache
	case model.contextPanel != nil:
		return focusContext
	case model.plugins.open:
		return focusPlugins
	case model.tasks.open:
		return focusTasks
	case model.detailOpen:
		return focusTranscript
	case model.title.editor != nil || model.title.request != nil:
		return focusTitle
	case model.planReview.open:
		return focusPlanReview
	case model.recalledSend != rewindBrowsing:
		return focusRecalledSend
	default:
		return focusComposer
	}
}

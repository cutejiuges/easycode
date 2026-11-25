package tui

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func promptSubmitHookDisclosure() []string {
	return []string{
		"The command reads each submitted prompt as plain text, including approved implementation text.",
		"Trust applies to later submissions in this session.",
		"This check can return a prompt for editing. Command output is discarded.",
	}
}

func (model Model) acceptPromptHookEvent(event protocol.Event, target domain.PromptSubmitTarget) bool {
	if model.cancelling || model.activeTurnID != target.TurnID || model.activeTurnSession != event.SessionID || !model.matchesChatScope(event) {
		return false
	}
	for _, input := range model.inputs {
		if input.id == target.InputID && input.turnID == target.TurnID && input.state == protocol.InputClaimed {
			return true
		}
	}
	return false
}

func (model *Model) projectPromptHookActivity(activity protocol.HookActivityPayload) {
	if activity.State == protocol.HookActivityChecking {
		if model.promptHookActivity == nil && model.hookTrust == nil {
			model.promptHookActivity = &activity
		}
		return
	}
	if current := model.promptHookActivity; current != nil && current.Target == activity.Target && current.PlanDigest == activity.PlanDigest && current.Reference == activity.Reference {
		model.promptHookActivity = nil
	}
}

func (model *Model) clearPromptHookInput(turn domain.TurnID, id domain.InputID) {
	if model.hookTrust != nil {
		if target, ok := model.hookTrust.request.Target.UserPromptSubmit(); ok && target.TurnID == turn && target.InputID == id {
			model.hookTrust = nil
		}
	}
	if model.promptHookActivity != nil {
		target, _ := model.promptHookActivity.Target.UserPromptSubmit()
		if target.TurnID == turn && target.InputID == id {
			model.promptHookActivity = nil
		}
	}
}

func (model *Model) clearPromptHookPresentation() {
	model.promptHookActivity = nil
	if model.hookTrust != nil && model.hookTrust.request.Declaration.Reference.Event == domain.HookUserPromptSubmit {
		model.hookTrust = nil
	}
}

func (model Model) promptHookStatus() string {
	if model.promptHookActivity == nil {
		return ""
	}
	ref := model.promptHookActivity.Reference
	return "Checking submitted prompt · " + string(ref.Source) + ":" + ref.ID
}

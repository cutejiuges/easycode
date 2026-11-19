package tui

import (
	"slices"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type compactionStage uint8

const (
	compactionPreparing compactionStage = iota
	compactionReviewing
	compactionChecking
	compactionSummarizing
	compactionCancelling
)

type compactionView struct {
	scope      domain.CompactionScope
	sessionID  domain.SessionID
	threadID   domain.ThreadID
	planDigest string
	trustIDs   [domain.MaxHookMatches]string
	trustCount int
	stage      compactionStage
}

func (model *Model) prepareCompaction(id domain.CompactionID, scope domain.CompactionScope) {
	model.activeCompactionID = id
	model.compaction = compactionView{scope: scope}
	model.compactionStatus = "Preparing context compaction…"
}

func (model *Model) clearCompaction() {
	if model.hookTrust != nil {
		id, _, ok := model.hookTrust.request.Target.Compaction()
		if ok && id == model.activeCompactionID {
			model.hookTrust = nil
		}
	}
	model.activeCompactionID = ""
	model.compaction = compactionView{}
}

func (model Model) freshAutomaticCompaction(scope domain.CompactionScope) bool {
	return scope.Trigger == domain.CompactionAuto && scope.TurnID == model.activeTurnID &&
		(scope.TurnID != model.lastAutomaticCompaction.TurnID || scope.SampleIndex > model.lastAutomaticCompaction.SampleIndex)
}

func (model Model) currentCompaction(event protocol.Event, scope domain.CompactionScope) bool {
	return model.activeCompactionID == event.CompactionID && model.compaction.scope == scope &&
		(!model.compaction.sessionID.Valid() || model.compaction.sessionID == event.SessionID && model.compaction.threadID == event.ThreadID)
}

func (model Model) requestCompactionCancellation() (tea.Model, tea.Cmd) {
	if model.compaction.stage == compactionCancelling {
		return model, nil
	}
	model.compaction.stage = compactionCancelling
	model.compactionStatus = "Cancelling compaction — waiting for cleanup"
	if model.compaction.scope.Trigger == domain.CompactionAuto {
		model.cancelling = true
	}
	return model, model.cancelActiveCompaction()
}

func (model Model) projectCompactionPresentation(event protocol.Event) (tea.Model, tea.Cmd) {
	scope, err := protocol.CompactionEventScope(event)
	if err != nil || !model.currentCompaction(event, scope) || !model.compaction.sessionID.Valid() || model.compaction.stage == compactionCancelling {
		return model, nil
	}
	switch event.Kind {
	case protocol.EventHookTrustRequested:
		request, err := protocol.DecodeHookTrustRequested(event)
		view := &model.compaction
		if err != nil || view.stage > compactionReviewing || model.hookTrust != nil ||
			(view.planDigest != "" && view.planDigest != request.PlanDigest) || view.trustCount == len(view.trustIDs) ||
			slices.Contains(view.trustIDs[:view.trustCount], request.RequestID) {
			return model, nil
		}
		view.trustIDs[view.trustCount] = request.RequestID
		view.trustCount++
		view.planDigest = request.PlanDigest
		view.stage = compactionReviewing
		model.clearEffortFeedback()
		model.compactionStatus = "Waiting for PreCompact trust review…"
		return model.openHookTrust(event.TurnID, request)
	case protocol.EventHookTrustResolved:
		return model.projectHookTrustResolution(event)
	case protocol.EventHookActivity:
		payload, err := protocol.DecodeHookActivity(event)
		if err != nil || model.compaction.stage >= compactionSummarizing || model.hookTrust != nil ||
			(model.compaction.planDigest != "" && model.compaction.planDigest != payload.PlanDigest) || payload.State != protocol.HookActivityChecking {
			return model, nil
		}
		model.compaction.planDigest = payload.PlanDigest
		model.compaction.stage = compactionChecking
		model.compactionStatus = "Running PreCompact checks…"
	case protocol.EventCompactionSummarizing:
		if model.hookTrust != nil {
			return model, nil
		}
		model.compaction.stage = compactionSummarizing
		model.compactionStatus = "Compacting conversation…"
	}
	return model, nil
}

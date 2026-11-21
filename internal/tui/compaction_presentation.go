package tui

import (
	"fmt"
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
	compactionPostReviewing
	compactionPostChecking
)

type compactionView struct {
	scope      domain.CompactionScope
	sessionID  domain.SessionID
	threadID   domain.ThreadID
	planDigest string
	trustIDs   [domain.MaxHookMatches]string
	trustCount int
	stage      compactionStage
	cancelling bool
	post       protocol.PostCompactInstalledPayload
	postResult string
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
	if model.compaction.cancelling {
		return model, nil
	}
	model.compaction.cancelling = true
	model.compactionStatus = "Cancelling compaction — waiting for cleanup"
	if model.compaction.post.CheckpointSequence != 0 {
		model.compactionStatus = "Conversation compacted · cancelling PostCompact — waiting for cleanup"
		if model.compaction.postResult != "" {
			model.compactionStatus = "Conversation compacted · " + model.compaction.postResult
		}
	}
	if model.compaction.scope.Trigger == domain.CompactionAuto {
		model.cancelling = true
	}
	return model, model.cancelActiveCompaction()
}

func (model Model) projectCompactionPresentation(event protocol.Event) (tea.Model, tea.Cmd) {
	scope, err := protocol.CompactionEventScope(event)
	if err != nil || !model.currentCompaction(event, scope) || !model.compaction.sessionID.Valid() {
		return model, nil
	}
	view := &model.compaction
	switch event.Kind {
	case protocol.EventPostCompactInstalled:
		model.applyPostCompactInstalled(event)
	case protocol.EventPostCompactClosed:
		model.applyPostCompactClosed(event)
	case protocol.EventHookTrustRequested:
		request, err := protocol.DecodeHookTrustRequested(event)
		if err != nil || !view.acceptsHook(request.Declaration.Reference.Event, request.PlanDigest, true) || model.hookTrust != nil ||
			view.trustCount == len(view.trustIDs) ||
			slices.Contains(view.trustIDs[:view.trustCount], request.RequestID) {
			return model, nil
		}
		if request.Declaration.Reference.Event == domain.HookPostCompact && view.trustCount == view.post.Count {
			return model, nil
		}
		view.trustIDs[view.trustCount] = request.RequestID
		view.trustCount++
		view.planDigest = request.PlanDigest
		model.clearEffortFeedback()
		if request.Declaration.Reference.Event == domain.HookPostCompact {
			model.compactionStatus = "Conversation compacted · waiting for PostCompact trust review…"
		} else {
			view.stage = compactionReviewing
			model.compactionStatus = "Waiting for PreCompact trust review…"
		}
		return model.openHookTrust(event.TurnID, request)
	case protocol.EventHookTrustResolved:
		if !view.cancelling {
			return model.projectHookTrustResolution(event)
		}
	case protocol.EventHookActivity:
		payload, err := protocol.DecodeHookActivity(event)
		if err != nil || payload.State != protocol.HookActivityChecking || model.hookTrust != nil ||
			!view.acceptsHook(payload.Reference.Event, payload.PlanDigest, false) {
			return model, nil
		}
		view.planDigest = payload.PlanDigest
		if payload.Reference.Event == domain.HookPostCompact {
			view.stage = compactionPostChecking
			model.compactionStatus = "Conversation compacted · running PostCompact checks…"
		} else {
			view.stage = compactionChecking
			model.compactionStatus = "Running PreCompact checks…"
		}
	case protocol.EventCompactionSummarizing:
		if view.stage >= compactionSummarizing {
			return model, nil
		}
		if model.hookTrust != nil {
			id, reviewScope, _ := model.hookTrust.request.Target.Compaction()
			if !view.cancelling || id != event.CompactionID || reviewScope != scope {
				return model, nil
			}
			// 摘要准入已越过先前的审查，取消期间仍须接收可能先提交的安装事实。
			model.hookTrust = nil
		}
		view.stage = compactionSummarizing
		if !view.cancelling {
			model.compactionStatus = "Compacting conversation…"
		}
	}
	return model, nil
}

func (view compactionView) acceptsHook(event domain.HookEvent, digest string, review bool) bool {
	if view.cancelling || view.postResult != "" || view.planDigest != "" && view.planDigest != digest {
		return false
	}
	switch event {
	case domain.HookPreCompact:
		return view.stage <= compactionReviewing || !review && view.stage == compactionChecking
	case domain.HookPostCompact:
		return view.stage == compactionPostReviewing || !review && view.stage == compactionPostChecking
	}
	return false
}

func (model *Model) applyPostCompactInstalled(event protocol.Event) {
	view := &model.compaction
	payload, err := protocol.DecodePostCompactInstalled(event)
	if err != nil || view.stage != compactionSummarizing || model.hookTrust != nil {
		return
	}
	view.post = payload
	view.stage = compactionPostReviewing
	view.planDigest = payload.PlanDigest
	view.trustIDs = [domain.MaxHookMatches]string{}
	view.trustCount = 0
	model.effortOperationGeneration++
	switch {
	case payload.SkipCode != "":
		view.postResult = "PostCompact skipped (" + string(payload.SkipCode) + ")"
		model.compactionStatus = "Conversation compacted · " + view.postResult
	case view.cancelling:
		model.compactionStatus = "Conversation compacted · cancelling PostCompact — waiting for cleanup"
	default:
		model.compactionStatus = "Conversation compacted · preparing PostCompact checks…"
	}
}

func (model *Model) applyPostCompactClosed(event protocol.Event) {
	view := &model.compaction
	payload, err := protocol.DecodePostCompactClosed(event)
	if err != nil || view.postResult != "" || view.post.PlanDigest != payload.PlanDigest ||
		view.post.CheckpointSequence != payload.CheckpointSequence || payload.Finished > view.post.Count ||
		payload.State == protocol.PostCompactSucceeded && payload.Finished != view.post.Count {
		return
	}
	if model.hookTrust != nil && model.hookTrust.request.PlanDigest == payload.PlanDigest {
		model.hookTrust = nil
	}
	outcome := string(payload.State)
	if payload.State == protocol.PostCompactUncertain {
		outcome = "outcome uncertain"
	}
	if payload.Code == domain.HookOK {
		view.postResult = fmt.Sprintf("PostCompact passed (%d/%d checks)", payload.Finished, view.post.Count)
	} else {
		view.postResult = fmt.Sprintf("PostCompact %s (%d/%d finished, %s)", outcome, payload.Finished, view.post.Count, payload.Code)
	}
	model.compactionStatus = "Conversation compacted · " + view.postResult
}

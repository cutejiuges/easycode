package tui

import (
	"fmt"
	"slices"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// sessionStartView 只保留当前操作可验证的计划、进度和关闭位置。
type sessionStartView struct {
	payload  protocol.SessionStartPayload
	session  domain.SessionID
	thread   domain.ThreadID
	requests [domain.MaxHookMatches]string
	count    int
	running  bool
}

func (view sessionStartView) present() bool { return view.payload.State != "" }
func (view sessionStartView) pending() bool {
	return view.payload.State == protocol.SessionStartPlanned
}

func (view sessionStartView) matches(event protocol.Event, target domain.SessionStartTarget, digest string) bool {
	return view.pending() && view.session == event.SessionID && view.thread == event.ThreadID &&
		view.payload.Target == target && view.payload.PlanDigest == digest
}

func (view *sessionStartView) accept(event protocol.Event, value protocol.SessionStartPayload) bool {
	if view.present() {
		if !view.matches(event, value.Target, value.PlanDigest) || value.State == protocol.SessionStartPlanned || value.Count > view.payload.Count {
			return false
		}
	} else if value.State != protocol.SessionStartPlanned && !(value.State == protocol.SessionStartSkipped && value.PlanDigest == "") {
		return false
	}
	view.payload, view.session, view.thread = value, event.SessionID, event.ThreadID
	return true
}

func (view *sessionStartView) acceptReview(event protocol.Event, request protocol.HookTrustRequestedPayload) bool {
	target, ok := request.Target.SessionStart()
	if !ok || !view.matches(event, target, request.PlanDigest) || view.running || view.count >= view.payload.Count ||
		slices.Contains(view.requests[:view.count], request.RequestID) {
		return false
	}
	view.requests[view.count] = request.RequestID
	view.count++
	return true
}

func (view *sessionStartView) acceptActivity(event protocol.Event, activity protocol.HookActivityPayload) bool {
	target, ok := activity.Target.SessionStart()
	if !ok || !view.matches(event, target, activity.PlanDigest) || activity.State != protocol.HookActivityChecking {
		return false
	}
	view.running = true
	return true
}

func (view sessionStartView) status() string {
	prefix := "SessionStart (" + string(view.payload.Target.Source) + ") · "
	switch view.payload.State {
	case protocol.SessionStartPlanned:
		if view.running {
			return prefix + "Running checks…"
		}
		return prefix + "Preparing checks…"
	case protocol.SessionStartApplied:
		return fmt.Sprintf("%sContext saved (%d) · Ctrl+O for details", prefix, view.payload.Count)
	case protocol.SessionStartSkipped:
		if view.payload.Code == domain.HookOK {
			return prefix + "No context added"
		}
		return prefix + "No context added (" + string(view.payload.Code) + ")"
	case protocol.SessionStartCancelled:
		return prefix + "Cancelled"
	case protocol.SessionStartUncertain:
		return prefix + "Context outcome uncertain"
	default:
		return ""
	}
}

func (model Model) projectSessionStart(event protocol.Event) (tea.Model, tea.Cmd) {
	value, err := protocol.DecodeSessionStart(event)
	if err != nil || !model.activeTurnID.Valid() || event.TurnID != model.activeTurnID || model.activeCompactionID.Valid() ||
		(model.activeTurnSession.Valid() && model.activeTurnSession != event.SessionID) ||
		(model.browser.thread.Valid() && model.browser.thread != event.ThreadID) ||
		!model.sessionStart.accept(event, value) {
		return model, nil
	}
	model.sessionStartStatus = model.sessionStart.status()
	return model.finishSessionStartPresentation(value)
}

func (model Model) projectCompactSessionStart(event protocol.Event) (tea.Model, tea.Cmd) {
	value, err := protocol.DecodeSessionStart(event)
	view := &model.compaction
	if err != nil || view.stage < compactionSummarizing || view.postUncertain ||
		(view.post.CheckpointSequence != 0 && (view.postResult == "" ||
			view.post.CheckpointSequence != value.Target.CheckpointSequence || view.post.Revision != value.Target.CheckpointRevision)) ||
		!view.sessionStart.accept(event, value) {
		return model, nil
	}
	if !view.cancelling || !view.sessionStart.pending() {
		model.compactionStatus = "Conversation compacted · " + view.sessionStart.status()
	} else {
		model.compactionStatus = "Conversation compacted · cancelling SessionStart — waiting for cleanup"
	}
	return model.finishSessionStartPresentation(value)
}

func (model Model) finishSessionStartPresentation(value protocol.SessionStartPayload) (tea.Model, tea.Cmd) {
	if value.State == protocol.SessionStartPlanned {
		return model, nil
	}
	if model.hookTrust != nil {
		target, ok := model.hookTrust.request.Target.SessionStart()
		if ok && target == value.Target && model.hookTrust.request.PlanDigest == value.PlanDigest {
			model.hookTrust = nil
		}
	}
	if value.State == protocol.SessionStartApplied {
		return model.refreshSessionStartTranscript(value.Sequence)
	}
	return model, nil
}

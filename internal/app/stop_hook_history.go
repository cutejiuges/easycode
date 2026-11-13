package app

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"fmt"
)

// StopHooksCommitted 复制事实后仅做有界索引更新，不读取 journal、配置或进程。
func (q *transcriptQueries) StopHooksCommitted(state session.ReplayedStop) error {
	if err := state.Validate(); err != nil {
		return err
	}
	state = state.Clone()
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stops == nil {
		q.stops = make(map[domain.AssistantSampleReference]session.ReplayedStop)
	}
	sample := state.Plan.Sample()
	if prior, ok := q.stops[sample]; ok && (prior.Plan.Digest() != state.Plan.Digest() || prior.DetailSequence >= state.DetailSequence) {
		return fmt.Errorf("invalid stop history update")
	}
	q.stops[sample] = state
	if q.stopSequences == nil {
		q.stopSequences = make(map[uint64]domain.AssistantSampleReference)
	}
	q.stopSequences[state.PlanSequence-2] = sample
	return nil
}

func (q *transcriptQueries) stopForNodeLocked(node transcriptNode) *session.ReplayedStop {
	sample, ok := q.stopSequences[node.cursor.Sequence]
	if node.item.HookSource != nil {
		sample, ok = node.item.HookSource.Identity.Target.AssistantSample()
	}
	if !ok {
		return nil
	}
	state, ok := q.stops[sample]
	if !ok {
		return nil
	}
	cloned := state.Clone()
	return &cloned
}
func stopSummary(state session.ReplayedStop) protocol.StopHookCommittedPayload {
	phase := protocol.StopHookPlanned
	switch {
	case state.DeliverySequence > 0:
		phase = protocol.StopHookDelivered
	case state.Decision != nil:
		phase = protocol.StopHookDecided
	case state.Started != nil:
		phase = protocol.StopHookStarted
	case len(state.Finished) > 0:
		phase = protocol.StopHookFinished
	}
	p := protocol.StopHookCommittedPayload{Target: state.Plan.Target(), PlanDigest: state.Plan.Digest(), DetailSequence: state.DetailSequence, Phase: phase}
	if state.Decision != nil {
		p.Disposition = state.Decision.Disposition
	}
	return p
}
func stopDetail(state session.ReplayedStop) string {
	text := fmt.Sprintf("Completion checks · sample %d · plan %s", state.Plan.Sample().SampleIndex, state.Plan.Digest())
	if state.Plan.Kind() == domain.StopPlanUnavailable {
		text += "\nUnavailable: " + string(state.Plan.UnavailableCode())
	}
	for _, f := range state.Finished {
		text += fmt.Sprintf("\n\nCheck %s:%s · %s (%s) · stdout %d bytes · stderr %d bytes", f.Feedback.Reference.Source, f.Feedback.Reference.ID, f.Result.Status, f.Result.Code, f.Result.StdoutBytes, f.Result.StderrBytes)
	}
	if state.Started != nil {
		text += "\n\nCheck in progress"
	}
	if state.Decision != nil {
		text += fmt.Sprintf("\n\nDecision: %s (%s) · processed %d/%d", state.Decision.Disposition, state.Decision.Code, state.Decision.Processed, len(state.Plan.Checks()))
		if feedback, ok := state.Decision.Feedback(); ok {
			text += "\n\nAuthorized model feedback:\n" + feedback.Text
		}
	}
	return text + "\n\nRaw hook output was not recorded."
}

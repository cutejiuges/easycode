package app

import (
	"errors"

	"easycode/internal/session"
	"easycode/internal/tool"
)

// PreHooksCommitted 在通知之前安装已提交快照，保留 O 并独立保存检查前缀与选择。
func (q *transcriptQueries) PreHooksCommitted(original tool.Invocation, sequence uint64, snapshot session.ReplayedHookGate) error {
	if sequence == 0 || snapshot.Validate(original) != nil || sequence < snapshot.PlanSequence || sequence < snapshot.StartedSequence || snapshot.Decision != nil && sequence != snapshot.DecisionSequence {
		return errors.New("invalid pre hook history snapshot")
	}
	cloned := snapshot.Clone()
	hooks := make([]session.HookHistoryEntry, len(cloned.Finished))
	for i, f := range cloned.Finished {
		hooks[i] = session.HookHistoryEntry{Reference: f.Feedback.Reference, Result: f.Result, Feedback: f.Feedback}
	}
	q.mu.Lock()
	stored := q.tools[original.InvocationID()]
	var previous session.ToolHistoryEntry
	if stored != nil {
		previous = *stored
	}
	q.mu.Unlock()
	if stored == nil || !previous.Invocation.Equal(original) || previous.HasResult || previous.PostHooks != nil || sequence <= previous.DetailSequence {
		return errors.New("pre hook history source or order mismatch")
	}
	if old := previous.PreHooks; old != nil {
		if old.Plan.Digest != cloned.Plan.Digest || old.Decision != nil || len(old.Finished) > len(cloned.Finished) {
			return errors.New("pre hook history prefix mismatch")
		}
		for i, f := range old.Finished {
			if !f.Equal(cloned.Finished[i]) {
				return errors.New("pre hook history changed a committed effect")
			}
		}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.invalid || stored.DetailSequence != previous.DetailSequence || stored.HasResult {
		q.invalid = true
		return errors.New("pre hook history snapshot is stale")
	}
	stored.PreHooks = &cloned
	stored.DetailSequence = sequence
	stored.PreDetailSequence = sequence
	stored.Hooks = hooks
	return nil
}

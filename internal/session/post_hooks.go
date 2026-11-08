package session

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
	"fmt"
)

func NewToolPostHookPlannedDraft(turn domain.TurnID, p domain.PostHookPlan) (RecordDraft, error) {
	if !turn.Valid() || p.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid post hook plan")
	}
	return encodeDraft(EventToolPostHookPlanned, "", turn, p.Clone())
}
func NewToolPostHookCompletedDraft(turn domain.TurnID, v domain.PostHookTerminal) (RecordDraft, error) {
	if !turn.Valid() || v.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid post hook terminal")
	}
	return encodeDraft(EventToolPostHookCompleted, "", turn, v.Clone())
}
func DecodeToolPostHookPlanned(record Record) (domain.PostHookPlan, error) {
	if record.EventKind != EventToolPostHookPlanned {
		return domain.PostHookPlan{}, fmt.Errorf("invalid post hook plan kind")
	}
	return domain.DecodePostHookPlan(record.Payload)
}
func DecodeToolPostHookCompleted(record Record) (domain.PostHookTerminal, error) {
	if record.EventKind != EventToolPostHookCompleted {
		return domain.PostHookTerminal{}, fmt.Errorf("invalid post hook terminal kind")
	}
	return domain.DecodePostHookTerminal(record.Payload)
}

// ReplayedPostHooks 与 Pre gate 独立；真实结果不会自动解除此屏障。
type ReplayedPostHooks struct {
	Plan             domain.PostHookPlan
	PlanSequence     uint64
	Started          *domain.HookRunIdentity
	StartedSequence  uint64
	Finished         []domain.HookFinished
	Terminal         *domain.PostHookTerminal
	TerminalSequence uint64
}

func (v ReplayedPostHooks) Clone() ReplayedPostHooks {
	v.Plan = v.Plan.Clone()
	v.Finished = append([]domain.HookFinished(nil), v.Finished...)
	if v.Started != nil {
		id := *v.Started
		v.Started = &id
	}
	if v.Terminal != nil {
		terminal := v.Terminal.Clone()
		v.Terminal = &terminal
	}
	return v
}
func (v ReplayedPostHooks) Feedback() []domain.FrozenHookFeedback {
	out := make([]domain.FrozenHookFeedback, 0, len(v.Finished)+domain.MaxHookMatches)
	for _, f := range v.Finished {
		out = append(out, f.Feedback)
	}
	if v.Terminal != nil {
		out = append(out, v.Terminal.SuffixFeedback...)
	}
	return out
}
func callComplete(call ReplayedToolCall) bool {
	return call.State == ReplayedToolCallResult && (call.PostHooks == nil || call.PostHooks.Terminal != nil)
}
func (v ReplayedPostHooks) Validate(call ReplayedToolCall) error {
	p := v.Plan.Plan
	if v.Plan.Validate() != nil || v.PlanSequence <= call.ReadySequence || p.InvocationID != string(call.Invocation.InvocationID()) || string(p.Capability) != string(call.Invocation.Capability()) || len(v.Finished) > len(p.Hooks) {
		return fmt.Errorf("invalid post hook recovery plan")
	}
	expectedDelivery := domain.HookDeliveryNone
	model := false
	for _, ref := range p.Hooks {
		model = model || ref.Feedback == domain.HookFeedbackModel
	}
	if model {
		expectedDelivery = domain.HookDeliveryUnavailable
		if tool.PostFeedbackAvailable(p, v.Plan.Reservation) {
			expectedDelivery = domain.HookDeliveryAvailable
		}
	}
	if v.Plan.Delivery != expectedDelivery {
		return fmt.Errorf("invalid post hook delivery reservation")
	}
	if call.Hooks != nil && (call.Hooks.Decision == nil || call.Hooks.Decision.Status != domain.HookContinue || call.Hooks.DecisionSequence >= v.PlanSequence) || call.ApprovalSequence != 0 && call.ApprovalSequence <= v.PlanSequence || call.StartedSequence != 0 && call.StartedSequence <= v.PlanSequence || call.ResultSequence != 0 && call.ResultSequence <= v.PlanSequence {
		return fmt.Errorf("post hook plan order is invalid")
	}
	if call.State == ReplayedToolCallResult && call.Result.Budget().Allowance() != v.Plan.Allowance-v.Plan.Reservation {
		return fmt.Errorf("post hook base allowance mismatch")
	}
	if len(v.Finished) > 0 || v.Started != nil {
		if call.State != ReplayedToolCallResult || call.StartedSequence == 0 || call.Result.Status() != tool.ResultSuccess {
			return fmt.Errorf("post hook has no executed successful result")
		}
	}
	var prior []domain.FrozenHookFeedback
	for i, f := range v.Finished {
		id, _ := domain.NewPostHookRunIdentity(v.Plan, i)
		if f.Validate() != nil || f.Result.Identity != id || f.Feedback.Reference != p.Hooks[i] || i < len(v.Finished)-1 && (f.Result.Status == domain.HookCancel || f.Result.Status == domain.HookUncertain) {
			return fmt.Errorf("invalid post hook execution prefix")
		}
		candidate := domain.HookFeedbackCandidate{}
		if f.Feedback.Kind == domain.HookFeedbackContent {
			candidate, _ = domain.NewHookFeedbackCandidate(f.Feedback.Content)
		}
		expected, err := tool.FreezePostFeedback(v.Plan, prior, f.Result, candidate)
		if err != nil || expected != f {
			return fmt.Errorf("invalid frozen post hook feedback")
		}
		prior = append(prior, f.Feedback)
	}
	if v.Started == nil && v.StartedSequence != 0 || v.Terminal == nil && v.TerminalSequence != 0 {
		return fmt.Errorf("orphan post hook sequence")
	}
	if v.Started != nil {
		id, err := domain.NewPostHookRunIdentity(v.Plan, len(v.Finished))
		if err != nil || *v.Started != id || v.StartedSequence <= call.ResultSequence || v.Terminal != nil {
			return fmt.Errorf("invalid pending post hook identity")
		}
	}
	if v.Terminal == nil {
		if len(v.Finished) == len(p.Hooks) || len(v.Finished) > 0 && (v.Finished[len(v.Finished)-1].Result.Status == domain.HookCancel || v.Finished[len(v.Finished)-1].Result.Status == domain.HookUncertain) {
			return fmt.Errorf("post hook finish requires atomic terminal")
		}
		return nil
	}
	t := v.Terminal
	if t.Validate() != nil || t.InvocationID != p.InvocationID || t.PlanDigest != v.Plan.Digest || t.Processed != len(v.Finished) || v.Started != nil || call.State != ReplayedToolCallResult || v.TerminalSequence <= call.ResultSequence {
		return fmt.Errorf("invalid post hook terminal evidence")
	}
	if len(v.Finished) > 0 {
		last := v.Finished[len(v.Finished)-1].Result
		if last.Status == domain.HookCancel && t.Disposition != domain.PostHookCancelled || last.Status == domain.HookUncertain && t.Disposition != domain.PostHookUncertain || last.Origin == "recovery" && t.Origin != "recovery" {
			return fmt.Errorf("post hook terminal differs from accepted interruption")
		}
	}
	if t.Origin == "recovery" {
		if t.Disposition == domain.PostHookCompleted || t.Disposition == domain.PostHookSkipped && t.Code != domain.HookToolNotSuccessful || t.Disposition == domain.PostHookCancelled && t.Code != domain.HookPostInterrupted || t.Disposition == domain.PostHookUncertain && (len(v.Finished) == 0 || v.Finished[len(v.Finished)-1].Result.Origin != "recovery") {
			return fmt.Errorf("invalid recovery post hook terminal")
		}
	} else if t.Code == domain.HookPostInterrupted {
		return fmt.Errorf("execution post hook terminal has recovery code")
	}
	switch t.Disposition {
	case domain.PostHookCompleted:
		if len(v.Finished) != len(p.Hooks) {
			return fmt.Errorf("incomplete post hook terminal")
		}
		code := domain.HookOK
		for _, f := range v.Finished {
			if f.Result.Status == domain.HookCancel || f.Result.Status == domain.HookUncertain {
				return fmt.Errorf("interrupted post hook completed")
			}
			if code == domain.HookOK && f.Result.Code != domain.HookOK {
				code = f.Result.Code
			}
		}
		if t.Code != code {
			return fmt.Errorf("post hook aggregate code mismatch")
		}
	case domain.PostHookSkipped:
		if len(v.Finished) >= len(p.Hooks) {
			return fmt.Errorf("post hook skip has no suffix")
		}
		if t.Code == domain.HookToolNotSuccessful && call.Result.Status() == tool.ResultSuccess || (t.Code == domain.HookInputLimit || t.Code == domain.HookExecutionBudgetExhausted) && (call.Result.Status() != tool.ResultSuccess || call.StartedSequence == 0) {
			return fmt.Errorf("post hook skip reason mismatch")
		}
		if t.Code == domain.HookTrustDenied || t.Code == domain.HookTrustRequired {
			if len(v.Finished) != 0 || call.StartedSequence != 0 || call.ApprovalSequence != 0 || call.Result.Code() != string(t.Code) || call.Result.Status() != tool.ResultDenied {
				return fmt.Errorf("post hook trust denial mismatch")
			}
		}
	case domain.PostHookUncertain:
		if len(v.Finished) == 0 || v.Finished[len(v.Finished)-1].Result.Status != domain.HookUncertain {
			return fmt.Errorf("post hook uncertainty has no accepted run")
		}
	case domain.PostHookCancelled:
		if call.Result.Status() != tool.ResultSuccess || call.StartedSequence == 0 {
			return fmt.Errorf("post hook cancellation has no successful tool")
		}
	}
	eligible := t.Origin == "execution" && call.Result.Status() == tool.ResultSuccess && call.StartedSequence != 0 && v.Plan.Delivery == domain.HookDeliveryAvailable
	expectedSuffix := 0
	if eligible {
		for _, ref := range p.Hooks[len(v.Finished):] {
			if ref.Feedback == domain.HookFeedbackModel {
				expectedSuffix++
			}
		}
	}
	if len(t.SuffixFeedback) != expectedSuffix {
		return fmt.Errorf("invalid post hook suffix feedback set")
	}
	for _, f := range t.SuffixFeedback {
		i := f.Identity.Ordinal
		id, err := domain.NewPostHookRunIdentity(v.Plan, i)
		if err != nil || f.Identity != id || f.Reference != p.Hooks[i] || i < len(v.Finished) || f.Kind != domain.HookFeedbackCode || f.Code != t.Code {
			return fmt.Errorf("invalid post hook suffix feedback")
		}
		before, err := tool.RenderPostFeedback(prior)
		if err != nil {
			return err
		}
		prior = append(prior, f)
		after, err := tool.RenderPostFeedback(prior)
		if err != nil || f.BudgetDelta != len(after)-len(before) {
			return fmt.Errorf("post hook suffix budget mismatch")
		}
	}
	text, err := tool.RenderPostFeedback(prior)
	if err != nil {
		return err
	}
	budget, err := domain.NewPostHookBudget(v.Plan.Allowance, v.Plan.Reservation, call.Result.Budget().ActualBytes(), len(text))
	if err != nil || budget != t.Budget || t.FeedbackDigest != domain.HookDigest([]byte(text)) {
		return fmt.Errorf("post hook terminal budget mismatch")
	}
	return nil
}

func (active *activeReplayTurn) postCall(invocation string) (*ReplayedToolCall, error) {
	for i := range active.pendingCalls {
		call := &active.pendingCalls[i]
		if string(call.Invocation.InvocationID()) != invocation {
			continue
		}
		for j := 0; j < i; j++ {
			if !callComplete(active.pendingCalls[j]) {
				return nil, replayCorrupt("post hook crossed unfinished call")
			}
		}
		if !call.Invocation.Capability().RequiresApproval() {
			return nil, replayCorrupt("post hook capability is unsupported")
		}
		return call, nil
	}
	return nil, replayCorrupt("post hook invocation is unknown")
}
func (active *activeReplayTurn) acceptPostHookBatch(batch []Record) error {
	if len(batch) == 0 || !batchHasTurn(batch, active.turnID) {
		return replayCorrupt("invalid post hook scope")
	}
	first := batch[0]
	var invocation string
	switch first.EventKind {
	case EventToolPostHookPlanned:
		p, err := DecodeToolPostHookPlanned(first)
		if err != nil || len(batch) != 1 {
			return replayCorrupt("invalid post hook plan batch")
		}
		call, err := active.postCall(p.Plan.InvocationID)
		if err != nil {
			return err
		}
		if call.PostHooks != nil || call.State != ReplayedToolCallReady || call.ApprovalSequence != 0 {
			return replayCorrupt("duplicate or late post hook plan")
		}
		if active.budget == nil {
			return replayCorrupt("post hook plan has no budget owner")
		}
		allowance, err := active.budget.Allowance(call.Invocation.Capability())
		if err != nil || allowance != p.Allowance {
			return replayCorrupt("post hook plan allowance differs from prior facts")
		}
		model := false
		for _, ref := range p.Plan.Hooks {
			model = model || ref.Feedback == domain.HookFeedbackModel
		}
		ticket, err := active.budget.Reserve(call.Invocation.Capability(), model)
		if err != nil {
			return replayCorrupt("post hook budget ticket admission failed")
		}
		active.budgetTicket = ticket
		call.PostHooks = &ReplayedPostHooks{Plan: p, PlanSequence: first.Sequence}
		if err := call.PostHooks.Validate(*call); err != nil {
			return replayCorrupt(err.Error())
		}
		active.lastSequence = first.Sequence
		return nil
	case EventHookExecutionStarted:
		id, err := DecodeHookExecutionStarted(first)
		if err != nil || len(batch) != 1 || id.Event != domain.HookPostToolUse {
			return replayCorrupt("invalid post hook start batch")
		}
		invocation = id.InvocationID
	case EventHookExecutionFinished:
		f, err := DecodeHookExecutionFinished(first)
		if err != nil || f.Result.Identity.Event != domain.HookPostToolUse {
			return replayCorrupt("invalid post hook finish")
		}
		invocation = f.Result.Identity.InvocationID
	case EventToolPostHookCompleted:
		t, err := DecodeToolPostHookCompleted(first)
		if err != nil || len(batch) != 1 {
			return replayCorrupt("invalid post hook terminal batch")
		}
		invocation = t.InvocationID
	default:
		return replayCorrupt("invalid post hook batch kind")
	}
	call, err := active.postCall(invocation)
	if err != nil {
		return err
	}
	g := call.PostHooks
	if g == nil || g.Terminal != nil {
		return replayCorrupt("post hook has no active plan")
	}
	switch first.EventKind {
	case EventHookExecutionStarted:
		id, _ := DecodeHookExecutionStarted(first)
		if g.Started != nil {
			return replayCorrupt("duplicate post hook start")
		}
		g.Started = &id
		g.StartedSequence = first.Sequence
	case EventHookExecutionFinished:
		f, _ := DecodeHookExecutionFinished(first)
		if g.Started == nil || *g.Started != f.Result.Identity {
			return replayCorrupt("post hook finish has no accepted start")
		}
		g.Finished = append(g.Finished, f)
		g.Started = nil
		g.StartedSequence = 0
		final := len(g.Finished) == len(g.Plan.Plan.Hooks) || f.Result.Status == domain.HookCancel || f.Result.Status == domain.HookUncertain
		if final {
			if len(batch) != 2 || batch[1].EventKind != EventToolPostHookCompleted {
				return replayCorrupt("post hook finish requires atomic terminal")
			}
			terminal, err := DecodeToolPostHookCompleted(batch[1])
			if err != nil {
				return err
			}
			g.Terminal = &terminal
			g.TerminalSequence = batch[1].Sequence
		} else if len(batch) != 1 {
			return replayCorrupt("invalid intermediate post hook finish batch")
		}
	case EventToolPostHookCompleted:
		terminal, _ := DecodeToolPostHookCompleted(first)
		g.Terminal = &terminal
		g.TerminalSequence = first.Sequence
	}
	if err := g.Validate(*call); err != nil {
		return replayCorrupt(err.Error())
	}
	delta := 0
	if first.EventKind == EventHookExecutionFinished {
		delta += g.Finished[len(g.Finished)-1].Feedback.BudgetDelta
	}
	if g.Terminal != nil {
		for _, f := range g.Terminal.SuffixFeedback {
			delta += f.BudgetDelta
		}
	}
	if first.EventKind != EventHookExecutionStarted && (active.budgetTicket == nil || active.budgetTicket.CommitFeedback(batch[len(batch)-1].Sequence, delta) != nil) {
		return replayCorrupt("post hook feedback budget mismatch")
	}
	if g.Terminal != nil {
		if err := active.budgetTicket.Close(); err != nil {
			return replayCorrupt("post hook budget ticket close failed")
		}
		active.budgetTicket = nil
	}
	active.lastSequence = batch[len(batch)-1].Sequence
	return nil
}

func validPostTrustResult(call ReplayedToolCall, result tool.InvocationResult) bool {
	return call.PostHooks != nil && call.StartedSequence == 0 && result.Status() == tool.ResultDenied && (result.Code() == string(domain.HookTrustDenied) || result.Code() == string(domain.HookTrustRequired))
}

func callCompletionSequence(call ReplayedToolCall) uint64 {
	if !callComplete(call) {
		return 0
	}
	sequence := call.ResultSequence
	if call.PostHooks != nil {
		sequence = max(sequence, call.PostHooks.TerminalSequence)
	}
	return sequence
}

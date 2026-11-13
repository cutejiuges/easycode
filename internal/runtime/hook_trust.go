package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
)

func (r *Runtime) ResolveHookTrust(response protocol.HookTrustResponse) error {
	if response.Validate() != nil || response.SessionID != r.sessionID || response.ThreadID != r.threadID {
		return fault.New(fault.CodeInvalidInput, "hook trust response mismatch")
	}
	r.hooks.mu.Lock()
	defer r.hooks.mu.Unlock()
	w := r.hooks.wait
	if w == nil || w.resolved || response.TurnID != w.turn || response.RequestID != w.request.RequestID || response.Target != w.request.Target || response.PlanDigest != w.request.PlanDigest || response.Reference != w.request.Declaration.Reference {
		return fault.New(fault.CodeInvalidInput, "hook trust response mismatch")
	}
	select {
	case <-w.cancelled:
		return fault.New(fault.CodeInvalidInput, "hook trust request cancelled")
	default:
	}
	w.decision = response.Decision
	w.resolved = true
	close(w.done)
	return nil
}
func (r *Runtime) ResetHookTrust(target protocol.HookTrustTarget) error {
	if target.Validate() != nil || r.active.Load() || target.SessionID != r.sessionID || target.ThreadID != r.threadID {
		return fault.New(fault.CodeInvalidInput, "hook trust reset rejected")
	}
	r.hooks.mu.Lock()
	defer r.hooks.mu.Unlock()
	if r.hooks.wait != nil {
		return fault.New(fault.CodeInvalidInput, "hook trust request active")
	}
	for _, d := range r.hooks.snapshot.Declarations() {
		if d.Reference == target.Reference {
			delete(r.hooks.trust, target.Reference.Key())
			return nil
		}
	}
	return fault.New(fault.CodeInvalidInput, "hook trust target mismatch")
}

func (r *Runtime) trustHook(ctx context.Context, emit Emitter, turn domain.TurnID, target domain.HookTarget, planDigest string, d domain.HookDeclaration) (domain.HookCode, error) {
	r.hooks.mu.Lock()
	decision := r.hooks.trust[d.Reference.Key()]
	r.hooks.mu.Unlock()
	if decision == protocol.HookTrustSession {
		return domain.HookOK, nil
	}
	if decision == protocol.HookTrustDeny {
		return domain.HookTrustDenied, nil
	}
	id, err := r.newApprovalRequestID()
	if err != nil {
		return domain.HookTrustDenied, err
	}
	p := protocol.HookTrustRequestedPayload{RequestID: string(id), Target: target, PlanDigest: planDigest, Declaration: d}
	event, err := protocol.NewHookTrustRequested(p)
	if err != nil {
		return domain.HookTrustDenied, err
	}
	w := &hookTrustWait{request: p, turn: turn, cancelled: ctx.Done(), done: make(chan struct{})}
	r.hooks.mu.Lock()
	if r.hooks.wait != nil {
		r.hooks.mu.Unlock()
		return domain.HookTrustDenied, fault.New(fault.CodeTurnFailed, "another hook trust request is active")
	}
	r.hooks.wait = w
	r.hooks.mu.Unlock()
	emit(r.decorate(event, turn))
	select {
	case <-w.done:
	case <-ctx.Done():
	}
	r.hooks.mu.Lock()
	decision = protocol.HookTrustDeny
	cancelled := false
	select {
	case <-w.cancelled:
		cancelled = true
	default:
	}
	if !cancelled && w.resolved {
		decision = w.decision
		r.hooks.trust[d.Reference.Key()] = decision
	}
	r.hooks.wait = nil
	r.hooks.mu.Unlock()
	if ctx.Err() != nil {
		return domain.HookCancelled, nil
	}
	response, _ := protocol.NewHookTrustResponse(r.sessionID, r.threadID, turn, p, decision)
	resolved, _ := protocol.NewHookTrustResolved(response)
	emit(r.decorate(resolved, turn))
	if decision == protocol.HookTrustSession {
		return domain.HookOK, nil
	}
	return domain.HookTrustDenied, nil
}

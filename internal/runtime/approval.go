package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/tool"
	"errors"
	"sync"
	"sync/atomic"
)

// approvalState 独占实例级活动请求和按 capability 隔离的授权。
type approvalState struct {
	mu         sync.Mutex
	active     *runtimeApproval
	patchGrant atomic.Bool
	writeGrant atomic.Bool
}

var (
	errNoActiveApproval = errors.New("no active approval")
	errApprovalMismatch = errors.New("approval response mismatch")
)

func (runtime *Runtime) ResolveToolApproval(response tool.ApprovalResponse) error {
	if response.Validate() != nil {
		return errApprovalMismatch
	}
	runtime.approvals.mu.Lock()
	defer runtime.approvals.mu.Unlock()
	active := runtime.approvals.active
	if active == nil || active.closed {
		return errNoActiveApproval
	}
	request := active.request
	if request.Capability() == tool.CapabilityExec && response.Decision() == tool.ApprovalAllowSession {
		return errApprovalMismatch
	}
	if response.RequestID() != request.RequestID() || response.TurnID() != request.TurnID() || response.InvocationID() != request.InvocationID() || response.Fingerprint() != request.Fingerprint() || active.resolved {
		return errApprovalMismatch
	}
	active.response = response
	active.resolved = true
	close(active.done)
	return nil
}

func (runtime *Runtime) waitForApproval(ctx context.Context, emit Emitter, turnID domain.TurnID, request tool.ApprovalRequest) (tool.ApprovalResponse, error) {
	active := &runtimeApproval{request: request, done: make(chan struct{})}
	runtime.approvals.mu.Lock()
	if runtime.approvals.active != nil {
		runtime.approvals.mu.Unlock()
		return tool.ApprovalResponse{}, fault.New(fault.CodeTurnFailed, "another approval is active")
	}
	runtime.approvals.active = active
	runtime.approvals.mu.Unlock()
	var presentation *protocol.ApprovalPresentation
	if request.Capability() != tool.CapabilityExec {
		value, err := approvalPresentation(request)
		if err != nil {
			runtime.approvals.mu.Lock()
			active.closed = true
			runtime.approvals.active = nil
			runtime.approvals.mu.Unlock()
			return tool.ApprovalResponse{}, err
		}
		presentation = &value
	}
	decisions := make([]protocol.ApprovalDecision, len(request.Decisions()))
	for index, decision := range request.Decisions() {
		decisions[index] = protocol.ApprovalDecision(decision)
	}
	event, err := protocol.NewToolApprovalRequested(protocol.ToolApprovalRequestedPayload{RequestID: string(request.RequestID()), InvocationID: string(request.InvocationID()), ProviderCallID: string(request.ProviderCallID()), Capability: string(request.Capability()), Reason: request.Reason(), RelativePath: request.RelativePath(), Fingerprint: string(request.Fingerprint()), Presentation: presentation, Decisions: decisions, Command: request.ExecInput().Command(), TimeoutMillis: request.ExecInput().TimeoutMillis()})
	if err != nil {
		runtime.approvals.mu.Lock()
		active.closed = true
		if runtime.approvals.active == active {
			runtime.approvals.active = nil
		}
		runtime.approvals.mu.Unlock()
		return tool.ApprovalResponse{}, fault.New(fault.CodeTurnFailed, "approval event is invalid")
	}
	emit(runtime.decorate(event, turnID))
	select {
	case <-active.done:
	case <-ctx.Done():
		runtime.approvals.mu.Lock()
		if !active.resolved {
			active.closed = true
			runtime.approvals.active = nil
			runtime.approvals.mu.Unlock()
			response, _ := tool.NewApprovalResponse(request.RequestID(), turnID, request.InvocationID(), request.Fingerprint(), tool.ApprovalAbort, "")
			return response, nil
		}
		runtime.approvals.mu.Unlock()
	}
	runtime.approvals.mu.Lock()
	response := active.response
	active.closed = true
	if runtime.approvals.active == active {
		runtime.approvals.active = nil
	}
	runtime.approvals.mu.Unlock()
	return response, nil
}

func (runtime *Runtime) emitApprovalResolved(emit Emitter, turnID domain.TurnID, request tool.ApprovalRequest, outcome tool.ApprovalOutcome) {
	// 非交互宿主只消费turn协议；没有展示过的审批不得产生UI收口事件。
	if !runtime.interactiveApprovals {
		return
	}
	decision := protocol.ApprovalDeny
	switch outcome {
	case tool.ApprovalOutcomeAllowOnce, tool.ApprovalOutcomeAllowFromSession:
		decision = protocol.ApprovalAllowOnce
	case tool.ApprovalOutcomeAllowSession:
		decision = protocol.ApprovalAllowSession
	case tool.ApprovalOutcomeAbort:
		decision = protocol.ApprovalAbort
	}
	event, err := protocol.NewToolApprovalResolved(protocol.ToolApprovalResolvedPayload{RequestID: string(request.RequestID()), InvocationID: string(request.InvocationID()), Fingerprint: string(request.Fingerprint()), Decision: decision})
	if err == nil {
		emit(runtime.decorate(event, turnID))
	}
}

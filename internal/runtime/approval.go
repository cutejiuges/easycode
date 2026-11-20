package runtime

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
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

func (state *approvalState) grant(capability tool.CapabilityID) *atomic.Bool {
	switch capability {
	case tool.CapabilityPatch:
		return &state.patchGrant
	case tool.CapabilityWrite:
		return &state.writeGrant
	default:
		return nil
	}
}

// approvalResolution 冻结实际决定及可关联的宿主发布事实。
type approvalResolution struct {
	outcome   tool.ApprovalOutcome
	source    session.ApprovalDecisionSource
	feedback  string
	published bool
}

type runtimeApproval struct {
	request    tool.ApprovalRequest
	decisions  []tool.ApprovalDecision
	cancelled  <-chan struct{}
	done       chan struct{}
	resolution approvalResolution
}

func interruptedApproval(published bool) approvalResolution {
	return approvalResolution{outcome: tool.ApprovalOutcomeAbort, source: session.ApprovalSourceInterruption, published: published}
}

// resolve 和 interrupt 只在 approvalState 持锁时调用。
func (active *runtimeApproval) resolve(decision approvalResolution) {
	active.resolution = decision
	close(active.done)
}

func (active *runtimeApproval) interrupt() {
	if active.resolution.outcome == "" {
		active.resolve(interruptedApproval(true))
	}
}

func (runtime *Runtime) interruptToolApproval(turnID domain.TurnID) {
	runtime.approvals.mu.Lock()
	defer runtime.approvals.mu.Unlock()
	if active := runtime.approvals.active; active != nil && active.request.TurnID() == turnID {
		active.interrupt()
	}
}

// ResolveToolApproval 在同一准入边界核对请求身份、实际选项与取消。
func (runtime *Runtime) ResolveToolApproval(response tool.ApprovalResponse) error {
	if response.Validate() != nil {
		return errApprovalMismatch
	}
	runtime.approvals.mu.Lock()
	defer runtime.approvals.mu.Unlock()
	active := runtime.approvals.active
	if active == nil || active.resolution.source == session.ApprovalSourceInterruption {
		return errNoActiveApproval
	}
	select {
	case <-active.cancelled:
		active.interrupt()
		return errNoActiveApproval
	default:
	}
	request := active.request
	if active.resolution.outcome != "" || response.RequestID() != request.RequestID() ||
		response.TurnID() != request.TurnID() || response.InvocationID() != request.InvocationID() ||
		response.Fingerprint() != request.Fingerprint() || !slices.Contains(active.decisions, response.Decision()) {
		return errApprovalMismatch
	}
	active.resolve(approvalResolution{
		outcome: tool.ApprovalOutcome(response.Decision()), source: session.ApprovalSourceInteractive,
		feedback: response.Feedback(), published: true,
	})
	return nil
}

func (runtime *Runtime) waitForApproval(ctx context.Context, emit Emitter, request tool.ApprovalRequest) (approvalResolution, error) {
	turnID := request.TurnID()
	reason := request.Reason()
	var rewrite protocol.PreInputProjection
	if state := runtime.toolTurn.toolCalls[request.InvocationID()]; state != nil && state.pre != nil {
		selection, err := state.selection()
		if err != nil {
			return approvalResolution{}, err
		}
		if selection.Changed() {
			rewrite, err = tool.ProjectPreInput(state.original, state.pre.Plan, state.pre.Finished, state.pre.Decision, state.detailSequence)
			if err != nil {
				return approvalResolution{}, err
			}
			reason += " PreToolUse input sources:"
			for _, f := range state.pre.Finished {
				if f.InputEffect.Kind() == domain.HookInputReplaced {
					reason += " " + string(f.Feedback.Reference.Source) + ":" + f.Feedback.Reference.ID
				}
			}
		}
	}
	var presentation *protocol.ApprovalPresentation
	if request.Capability() != tool.CapabilityExec {
		value, err := approvalPresentation(request)
		if err != nil {
			return approvalResolution{}, err
		}
		presentation = &value
	}
	offered := request.Decisions()
	decisions := make([]protocol.ApprovalDecision, len(offered))
	for index, decision := range offered {
		decisions[index] = protocol.ApprovalDecision(decision)
	}
	event, err := protocol.NewToolApprovalRequested(protocol.ToolApprovalRequestedPayload{InputRewrite: rewrite, RequestID: string(request.RequestID()), InvocationID: string(request.InvocationID()), ProviderCallID: string(request.ProviderCallID()), Capability: string(request.Capability()), Reason: reason, RelativePath: request.RelativePath(), Fingerprint: string(request.Fingerprint()), Presentation: presentation, Decisions: decisions, Command: request.ExecInput().Command(), TimeoutMillis: request.ExecInput().TimeoutMillis()})
	if err != nil {
		return approvalResolution{}, fault.New(fault.CodeTurnFailed, "approval event is invalid")
	}
	active := &runtimeApproval{request: request, decisions: offered, cancelled: ctx.Done(), done: make(chan struct{})}
	runtime.approvals.mu.Lock()
	if runtime.approvals.active != nil {
		runtime.approvals.mu.Unlock()
		return approvalResolution{}, fault.New(fault.CodeTurnFailed, "another approval is active")
	}
	select {
	case <-active.cancelled:
		runtime.approvals.mu.Unlock()
		return interruptedApproval(false), nil
	default:
	}
	runtime.approvals.active = active
	runtime.approvals.mu.Unlock()

	emit(runtime.decorate(event, turnID))
	select {
	case <-active.done:
	case <-ctx.Done():
	}
	runtime.approvals.mu.Lock()
	active.interrupt()
	decision := active.resolution
	runtime.approvals.active = nil
	runtime.approvals.mu.Unlock()
	return decision, nil
}

func (runtime *Runtime) decideApproval(ctx context.Context, emit Emitter, request tool.ApprovalRequest, policy tool.PolicyDecision) (approvalResolution, error) {
	switch policy {
	case tool.PolicyAllow:
		return approvalResolution{outcome: tool.ApprovalOutcomeAllowOnce, source: session.ApprovalSourcePolicy}, nil
	case tool.PolicyAsk:
		if grant := runtime.approvals.grant(request.Capability()); grant != nil && grant.Load() {
			return approvalResolution{outcome: tool.ApprovalOutcomeAllowFromSession, source: session.ApprovalSourceSessionGrant}, nil
		}
		if runtime.interactiveApprovals {
			return runtime.waitForApproval(ctx, emit, request)
		}
		if request.Capability() == tool.CapabilityExec && ctx.Err() != nil {
			return interruptedApproval(false), nil
		}
		return approvalResolution{outcome: tool.ApprovalOutcomeDeny, source: session.ApprovalSourceHeadless}, nil
	default:
		return approvalResolution{outcome: tool.ApprovalOutcomeDeny, source: session.ApprovalSourcePolicy}, nil
	}
}

func (resolution approvalResolution) rejection(label string, invocation tool.Invocation) tool.InvocationResult {
	status, code, verb := tool.ResultDenied, "approval_denied", "denied"
	if resolution.outcome == tool.ApprovalOutcomeAbort {
		status, code, verb = tool.ResultCancelled, "user_aborted", "cancelled"
	} else if resolution.source == session.ApprovalSourceHeadless {
		code = "approval_required"
	}
	if invocation.Capability() == tool.CapabilityExec {
		verb = "stopped"
	}
	message := label + " " + verb + ": " + code
	if resolution.feedback != "" {
		message += "\nUser feedback: " + resolution.feedback
	}
	return tool.NewInvocationErrorResult(invocation, status, code, message)
}

func (runtime *Runtime) emitApprovalResolved(emit Emitter, request tool.ApprovalRequest, resolution approvalResolution) {
	// 非交互宿主只消费turn协议；没有展示过的审批不得产生UI收口事件。
	if !resolution.published {
		return
	}
	decision := protocol.ApprovalDecision(resolution.outcome)
	event, err := protocol.NewToolApprovalResolved(protocol.ToolApprovalResolvedPayload{RequestID: string(request.RequestID()), InvocationID: string(request.InvocationID()), Fingerprint: string(request.Fingerprint()), Decision: decision})
	if err == nil {
		emit(runtime.decorate(event, request.TurnID()))
	}
}

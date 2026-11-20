package runtime

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/session"
	"easycode/internal/tool"
	"encoding/json"
)

// toolTurnState 集中由 turn owner 维护的工具调用、输出预算与样本展示状态。
type toolTurnState struct {
	toolCalls              map[tool.InvocationID]*toolExecutionCall
	activityEmitter        Emitter
	activityInvocations    map[tool.InvocationID]tool.Invocation
	resultBudget           *tool.ResultBudget
	resultTicket           *tool.ResultBudgetTicket
	resultTicketInvocation tool.InvocationID
	resultTicketCapability tool.CapabilityID
	resultTicketPost       bool
}

func (s *toolTurnState) resetResultTicket() {
	s.resultTicket = nil
	s.resultTicketInvocation = ""
	s.resultTicketCapability = ""
	s.resultTicketPost = false
}

// toolExecutionCall 由唯一 turn owner 持有，最多保留本 turn 已准入的真实调用。
type toolExecutionCall struct {
	origin         tool.ToolCallOrigin
	original       tool.Invocation
	sourceDigest   string
	pre            *session.ReplayedHookGate
	detailSequence uint64
	admission      tool.ExecutionAdmission
	resultOrigin   tool.InputResultOrigin
	result         tool.InvocationResult
	resultDigest   string
	resultSequence uint64
	hasResult      bool
	running        bool
}

func (r *Runtime) registerToolCall(origin tool.ToolCallOrigin, inv tool.Invocation) error {
	if origin.Validate() != nil || origin.SessionID != r.sessionID || origin.ThreadID != r.threadID || inv.Validate() != nil {
		return fault.New(fault.CodeStreamProtocol, "invalid tool call source")
	}
	if r.toolTurn.toolCalls == nil {
		r.toolTurn.toolCalls = make(map[tool.InvocationID]*toolExecutionCall)
	}
	if _, exists := r.toolTurn.toolCalls[inv.InvocationID()]; exists || len(r.toolTurn.toolCalls) >= maxToolCallsPerTurn {
		return fault.New(fault.CodeStreamProtocol, "duplicate or excessive tool call source")
	}
	digest, err := toolCallSourceDigest(origin, inv)
	if err != nil {
		return err
	}
	r.toolTurn.toolCalls[inv.InvocationID()] = &toolExecutionCall{origin: origin, original: inv.Clone(), sourceDigest: digest, resultOrigin: tool.InputResultExecution}
	return nil
}
func (r *Runtime) executionCall(turn domain.TurnID, inv tool.Invocation) (*toolExecutionCall, error) {
	state := r.toolTurn.toolCalls[inv.InvocationID()]
	if state == nil || state.origin.SessionID != r.sessionID || state.origin.ThreadID != r.threadID || state.origin.TurnID != turn || !state.original.Equal(inv) {
		return nil, fault.New(fault.CodeStreamProtocol, "tool invocation source mismatch")
	}
	return state, nil
}
func (state *toolExecutionCall) selection() (tool.InputSelection, error) {
	digest, err := toolCallSourceDigest(state.origin, state.original)
	if err != nil || digest != state.sourceDigest {
		return tool.InputSelection{}, fault.New(fault.CodeStreamProtocol, "tool call source fingerprint mismatch")
	}
	return (session.ReplayedToolCall{Origin: state.origin, Invocation: state.original, Hooks: state.pre}).InputSelection()
}

func toolCallSourceDigest(origin tool.ToolCallOrigin, invocation tool.Invocation) (string, error) {
	if origin.Validate() != nil {
		return "", fault.New(fault.CodeStreamProtocol, "invalid tool call origin")
	}
	ready, err := session.NewToolCallReadyDraft(origin.TurnID, invocation, origin.SampleIndex, origin.CallIndex)
	if err != nil {
		return "", err
	}
	encoded, err := codec.MarshalStable(struct {
		Origin tool.ToolCallOrigin `json:"origin"`
		Ready  json.RawMessage     `json:"ready"`
	}{origin, ready.PayloadBytes()})
	if err != nil {
		return "", err
	}
	return domain.HookDigest(encoded), nil
}
func (state *toolExecutionCall) evidence() (tool.InputResultEvidence, error) {
	selection, err := state.selection()
	if err != nil {
		return tool.InputResultEvidence{}, err
	}
	return tool.NewInputResultEvidence(selection, state.admission, state.resultOrigin)
}
func (r *Runtime) resultEvidence(result tool.InvocationResult) (tool.InputResultEvidence, error) {
	state := r.toolTurn.toolCalls[result.InvocationID()]
	if state == nil {
		return tool.InputResultEvidence{}, fault.New(fault.CodeStreamProtocol, "tool result has no ready source")
	}
	return state.evidence()
}
func (r *Runtime) registerExecutionAdmission(inv tool.Invocation, sequence uint64) (tool.ExecutionAdmission, error) {
	state := r.toolTurn.toolCalls[inv.InvocationID()]
	if state == nil || state.admission.Sequence() != 0 || sequence == 0 || sequence <= state.detailSequence {
		return tool.ExecutionAdmission{}, fault.New(fault.CodeStreamProtocol, "tool admission has no unique ready source")
	}
	selection, err := state.selection()
	if err != nil {
		return tool.ExecutionAdmission{}, err
	}
	effective, ok := selection.EffectiveInvocation()
	if !ok || !effective.Equal(inv) {
		return tool.ExecutionAdmission{}, fault.New(fault.CodeStreamProtocol, "tool admission input mismatch")
	}
	admission, err := tool.NewExecutionAdmission(inv.InvocationID(), sequence)
	if err != nil {
		return tool.ExecutionAdmission{}, err
	}
	state.admission = admission
	if post := r.hooks.post[inv.InvocationID()]; post != nil {
		post.admission = admission
	}
	if r.toolHistory != nil {
		if err := r.toolHistory.ToolStarted(inv.InvocationID(), admission); err != nil {
			r.poisoned.Store(true)
			return tool.ExecutionAdmission{}, err
		}
	}
	return admission, nil
}

// rememberToolResult 只在 Sync 后安装冻结结果及摘要，重入不再执行或重新关闭预算。
func (r *Runtime) rememberToolResult(turn domain.TurnID, result tool.InvocationResult) error {
	state := r.toolTurn.toolCalls[result.InvocationID()]
	if state == nil || state.origin.TurnID != turn || state.hasResult {
		return fault.New(fault.CodeStreamProtocol, "tool result source mismatch")
	}
	evidence, err := state.evidence()
	if err != nil || evidence.ValidateResult(result) != nil {
		return fault.New(fault.CodeStreamProtocol, "invalid frozen tool execution evidence")
	}
	draft, err := session.NewToolCallResultDraft(turn, result)
	if err != nil {
		return err
	}
	state.result = result
	state.resultDigest = domain.HookDigest(draft.PayloadBytes())
	state.hasResult = true
	return nil
}
func (state *toolExecutionCall) cachedResult() (tool.InvocationResult, error) {
	evidence, err := state.evidence()
	if err != nil || !state.hasResult || evidence.ValidateResult(state.result) != nil {
		return tool.InvocationResult{}, fault.New(fault.CodeStreamProtocol, "invalid cached tool execution evidence")
	}
	draft, err := session.NewToolCallResultDraft(state.origin.TurnID, state.result)
	if err != nil || domain.HookDigest(draft.PayloadBytes()) != state.resultDigest {
		return tool.InvocationResult{}, fault.New(fault.CodeStreamProtocol, "cached tool result mismatch")
	}
	return state.result, nil
}

// resetToolTurn 只在 turn 退役或会话 owner 已停止时释放调用事实缓存。
func (r *Runtime) resetToolTurn() {
	r.toolTurn.toolCalls = make(map[tool.InvocationID]*toolExecutionCall)
	r.resetHookTurn()
}

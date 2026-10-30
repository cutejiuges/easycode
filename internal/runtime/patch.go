package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/session"
	"easycode/internal/tool"
	"errors"
)

func (runtime *Runtime) executePatchCall(ctx context.Context, emit Emitter, turnID domain.TurnID, invocation tool.Invocation) (tool.InvocationResult, bool, error) {
	patch, ok := invocation.Patch()
	if !ok || runtime.patchPreparer == nil || runtime.patchExecutor == nil {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Patch runtime is not configured")
	}
	candidate, err := runtime.patchPreparer.Prepare(ctx, patch)
	if err != nil {
		code := "patch_prepare_failed"
		type coded interface{ Code() string }
		var value coded
		if errors.As(err, &value) && value.Code() != "" {
			code = value.Code()
		}
		result := tool.NewPatchErrorResult(patch, tool.ResultError, code, "Patch failed: "+code)
		if persistErr := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result); persistErr != nil {
			return tool.InvocationResult{}, false, persistErr
		}
		return result, false, nil
	}
	if candidate.Validate() != nil || candidate.InvocationID() != patch.InvocationID() || candidate.ProviderCallID() != patch.ProviderCallID() || candidate.WireKind() != patch.WireKind() || !candidate.Input().Equal(patch.Input()) {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Patch candidate does not match invocation")
	}
	requestID, err := runtime.newApprovalRequestID()
	if err != nil || !requestID.Valid() {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "approval request identity generation failed")
	}
	request, err := tool.NewApprovalRequest(requestID, runtime.sessionID, runtime.threadID, turnID, candidate, "Patch modifies an existing workspace file.", []tool.ApprovalDecision{tool.ApprovalAllowOnce, tool.ApprovalAllowSession, tool.ApprovalDeny})
	if err != nil {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "approval request is invalid")
	}
	outcome := tool.ApprovalOutcomeDeny
	feedback := ""
	decision := runtime.toolPolicy.Decide(callFromPatchInvocation(patch))
	source := session.ApprovalSourcePolicy
	if decision == tool.PolicyAsk && !runtime.interactiveApprovals {
		source = session.ApprovalSourceHeadless
	}
	if decision == tool.PolicyAsk && runtime.approvals.patchGrant.Load() {
		outcome = tool.ApprovalOutcomeAllowFromSession
		source = session.ApprovalSourceSessionGrant
	} else if decision == tool.PolicyAllow {
		outcome = tool.ApprovalOutcomeAllowOnce
		source = session.ApprovalSourcePolicy
	} else if decision == tool.PolicyAsk && runtime.interactiveApprovals {
		source = session.ApprovalSourceInteractive
		response, waitErr := runtime.waitForApproval(ctx, emit, turnID, request)
		if waitErr != nil {
			return tool.InvocationResult{}, false, waitErr
		}
		feedback = response.Feedback()
		switch response.Decision() {
		case tool.ApprovalAllowOnce:
			outcome = tool.ApprovalOutcomeAllowOnce
		case tool.ApprovalAllowSession:
			outcome = tool.ApprovalOutcomeAllowSession
		case tool.ApprovalDeny:
			outcome = tool.ApprovalOutcomeDeny
		case tool.ApprovalAbort:
			outcome = tool.ApprovalOutcomeAbort
		}
	}
	metadata, err := session.NewApprovalMetadata(invocation)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	fact := session.ToolApprovalDecidedPayload{RequestID: requestID, InvocationID: invocation.InvocationID(), Fingerprint: candidate.Fingerprint(), Outcome: outcome, Feedback: feedback, Source: source, Metadata: metadata}
	if outcome == tool.ApprovalOutcomeDeny || outcome == tool.ApprovalOutcomeAbort {
		status, code, preview := tool.ResultDenied, "approval_denied", "Patch denied: approval_denied"
		if outcome == tool.ApprovalOutcomeAbort {
			status = tool.ResultCancelled
			code = "user_aborted"
			preview = "Patch cancelled: user_aborted"
		} else if decision == tool.PolicyAsk && !runtime.interactiveApprovals {
			code = "approval_required"
			preview = "Patch denied: approval_required"
		} else if feedback != "" {
			preview += "\nUser feedback: " + feedback
		}
		result := tool.NewPatchErrorResult(patch, status, code, preview)
		factDraft, draftErr := session.NewToolApprovalDecidedDraft(turnID, fact)
		if draftErr != nil {
			return tool.InvocationResult{}, false, draftErr
		}
		result, freezeErr := runtime.freezeResult(result)
		if freezeErr != nil {
			return tool.InvocationResult{}, false, freezeErr
		}
		resultDraft, draftErr := session.NewToolCallResultDraft(turnID, result)
		if draftErr != nil {
			return tool.InvocationResult{}, false, draftErr
		}
		if _, appendErr := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{factDraft, resultDraft}); appendErr != nil {
			runtime.poisoned.Store(true)
			return tool.InvocationResult{}, false, fault.Wrap(fault.CodeSessionWrite, "persist Patch denial failed", appendErr)
		}
		runtime.notifyToolResult(turnID, result)
		runtime.emitApprovalResolved(emit, turnID, request, outcome)
		return result, outcome == tool.ApprovalOutcomeAbort, nil
	}
	factDraft, err := session.NewToolApprovalDecidedDraft(turnID, fact)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	if _, err = runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{factDraft}); err != nil {
		runtime.poisoned.Store(true)
		return tool.InvocationResult{}, false, fault.Wrap(fault.CodeSessionWrite, "persist Patch approval failed", err)
	}
	if outcome == tool.ApprovalOutcomeAllowSession {
		runtime.approvals.patchGrant.Store(true)
	}
	runtime.emitApprovalResolved(emit, turnID, request, outcome)
	result, err := runtime.executeExclusive(ctx, turnID, invocation, exclusiveAction{
		execute: func(ctx context.Context) tool.InvocationResult {
			runtime.readObservations.invalidate(candidate.RelativePath())
			return runtime.patchExecutor.Execute(ctx, candidate)
		},
		matches: func(result tool.InvocationResult) bool {
			return result.Status() != tool.ResultSuccess || result.PatchMetadata() == candidate.ResultMetadata()
		},
	})
	return result, false, err
}

func callFromPatchInvocation(invocation tool.PatchInvocation) tool.ReadyCall {
	call, _ := tool.NewPatchReadyCall(invocation.ProviderCallID(), invocation.WireKind(), invocation.Input())
	return call
}

package runtime

import (
	"context"
	"errors"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func (runtime *Runtime) executeWriteCall(ctx context.Context, emit Emitter, turnID domain.TurnID, invocation tool.Invocation) (tool.InvocationResult, bool, error) {
	v, ok := invocation.Write()
	if !ok || runtime.writePreparer == nil || runtime.writeExecutor == nil {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Write runtime is not configured")
	}
	finish := func(result tool.InvocationResult) (tool.InvocationResult, bool, error) {
		if err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, result); err != nil {
			return tool.InvocationResult{}, false, err
		}
		return result, false, nil
	}
	if ctx.Err() != nil {
		return finish(tool.NewWriteErrorResult(v, tool.ResultCancelled, "cancelled", "Write cancelled"))
	}
	candidate, err := runtime.writePreparer.Prepare(ctx, v, runtime.readObservations.lookup(v.Input().FilePath()))
	if err != nil {
		code := "write_prepare_failed"
		status := tool.ResultError
		type coded interface {
			Code() string
			Status() tool.ResultStatus
		}
		var value coded
		if errors.As(err, &value) && value.Code() != "" {
			code = value.Code()
			status = value.Status()
		}
		if status != tool.ResultError && status != tool.ResultConflict && status != tool.ResultCancelled {
			status = tool.ResultError
			code = "write_prepare_failed"
		}
		if status == tool.ResultConflict {
			runtime.readObservations.invalidate(v.Input().FilePath())
		}
		return finish(tool.NewWriteErrorResult(v, status, code, "Write failed: "+code))
	}
	if candidate.Validate() != nil || candidate.Invocation() != v {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Write candidate does not match invocation")
	}
	id, err := runtime.newApprovalRequestID()
	if err != nil || !id.Valid() {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "approval request identity generation failed")
	}
	request, err := tool.NewWriteApprovalRequest(id, runtime.sessionID, runtime.threadID, turnID, candidate)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	call, _ := tool.NewWriteReadyCall(v.ProviderCallID(), v.Input())
	decision := runtime.toolPolicy.Decide(call)
	outcome := tool.ApprovalOutcomeDeny
	feedback := ""
	if decision == tool.PolicyAllow {
		outcome = tool.ApprovalOutcomeAllowOnce
	} else if decision == tool.PolicyAsk {
		if runtime.writeGrant.Load() {
			outcome = tool.ApprovalOutcomeAllowFromSession
		} else if runtime.interactiveApprovals {
			response, err := runtime.waitForApproval(ctx, emit, turnID, request)
			if err != nil {
				return tool.InvocationResult{}, false, err
			}
			feedback = response.Feedback()
			switch response.Decision() {
			case tool.ApprovalAllowOnce:
				outcome = tool.ApprovalOutcomeAllowOnce
			case tool.ApprovalAllowSession:
				outcome = tool.ApprovalOutcomeAllowSession
			case tool.ApprovalAbort:
				outcome = tool.ApprovalOutcomeAbort
			}
		}
	}
	fact := session.ToolApprovalDecidedPayload{RequestID: id, InvocationID: v.InvocationID(), Fingerprint: candidate.Fingerprint(), Outcome: outcome, Feedback: feedback}
	factDraft, err := session.NewToolApprovalDecidedDraft(turnID, fact)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	if outcome == tool.ApprovalOutcomeDeny || outcome == tool.ApprovalOutcomeAbort {
		status, code, message := tool.ResultDenied, "approval_denied", "Write denied: approval_denied"
		if outcome == tool.ApprovalOutcomeAbort {
			status, code, message = tool.ResultCancelled, "user_aborted", "Write cancelled: user_aborted"
		} else if !runtime.interactiveApprovals {
			code, message = "approval_required", "Write denied: approval_required"
		} else if feedback != "" {
			message += "\nUser feedback: " + feedback
		}
		result := tool.NewWriteErrorResult(v, status, code, message)
		resultDraft, err := session.NewToolCallResultDraft(turnID, result)
		if err != nil {
			return tool.InvocationResult{}, false, err
		}
		if _, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{factDraft, resultDraft}); err != nil {
			runtime.poisoned.Store(true)
			return tool.InvocationResult{}, false, fault.Wrap(fault.CodeSessionWrite, "persist Write denial failed", err)
		}
		runtime.emitApprovalResolved(emit, turnID, request, outcome)
		return result, outcome == tool.ApprovalOutcomeAbort, nil
	}
	if _, err := runtime.journal.AppendBatch(context.WithoutCancel(ctx), []session.RecordDraft{factDraft}); err != nil {
		runtime.poisoned.Store(true)
		return tool.InvocationResult{}, false, fault.Wrap(fault.CodeSessionWrite, "persist Write approval failed", err)
	}
	if outcome == tool.ApprovalOutcomeAllowSession {
		runtime.writeGrant.Store(true)
	}
	runtime.emitApprovalResolved(emit, turnID, request, outcome)
	if ctx.Err() != nil {
		return finish(tool.NewWriteErrorResult(v, tool.ResultCancelled, "cancelled", "Write cancelled before execution"))
	}
	started, err := session.NewToolExecutionStartedDraft(turnID, v.InvocationID())
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	if _, err := runtime.journal.AppendBatch(ctx, []session.RecordDraft{started}); err != nil {
		if errors.Is(err, context.Canceled) && !runtime.journal.Poisoned() {
			return finish(tool.NewWriteErrorResult(v, tool.ResultCancelled, "cancelled", "Write cancelled before execution"))
		}
		runtime.poisoned.Store(true)
		return tool.InvocationResult{}, false, fault.Wrap(fault.CodeSessionWrite, "persist Write execution start failed", err)
	}
	runtime.readObservations.invalidate(candidate.RelativePath())
	result := runtime.writeExecutor.Execute(ctx, candidate)
	if result.Validate() != nil || result.InvocationID() != v.InvocationID() || result.ProviderCallID() != v.ProviderCallID() || result.Capability() != tool.CapabilityWrite || result.WireKind() != tool.WireStructured || (result.Status() == tool.ResultSuccess && result.WriteMetadata() != candidate.ResultMetadata()) {
		result = tool.NewWriteErrorResult(v, tool.ResultOutcomeUncertain, "invalid_tool_result", "Write failed: execution outcome is uncertain")
	}
	if result.Status() == tool.ResultOutcomeUncertain {
		runtime.poisoned.Store(true)
		runtime.readObservations.clear()
	}
	if result.Status() == tool.ResultConflict {
		runtime.readObservations.invalidate(candidate.RelativePath())
	}
	if _, _, err := finish(result); err != nil {
		return tool.InvocationResult{}, false, err
	}
	if result.Status() == tool.ResultOutcomeUncertain {
		return result, false, fault.New(fault.CodeSessionWrite, "Write outcome is uncertain")
	}
	return result, false, nil
}

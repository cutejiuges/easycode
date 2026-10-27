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
		if runtime.approvals.writeGrant.Load() {
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
	source := session.ApprovalSourcePolicy
	if decision == tool.PolicyAsk {
		source = session.ApprovalSourceHeadless
		if runtime.interactiveApprovals {
			source = session.ApprovalSourceInteractive
		}
	}
	if outcome == tool.ApprovalOutcomeAbort {
		source = session.ApprovalSourceInterruption
	} else if outcome == tool.ApprovalOutcomeAllowFromSession {
		source = session.ApprovalSourceSessionGrant
	}
	metadata, err := session.NewApprovalMetadata(invocation)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	fact := session.ToolApprovalDecidedPayload{RequestID: id, InvocationID: v.InvocationID(), Fingerprint: candidate.Fingerprint(), Outcome: outcome, Feedback: feedback, Source: source, Metadata: metadata}
	factDraft, err := session.NewToolApprovalDecidedDraft(turnID, fact)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	if outcome == tool.ApprovalOutcomeDeny || outcome == tool.ApprovalOutcomeAbort {
		status, code, message := tool.ResultDenied, "approval_denied", "Write denied: approval_denied"
		if outcome == tool.ApprovalOutcomeAbort {
			status, code, message = tool.ResultCancelled, "user_aborted", "Write cancelled: user_aborted"
		} else if decision == tool.PolicyAsk && !runtime.interactiveApprovals {
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
		runtime.approvals.writeGrant.Store(true)
	}
	runtime.emitApprovalResolved(emit, turnID, request, outcome)
	result, err := runtime.executeExclusive(ctx, turnID, invocation, exclusiveAction{
		execute: func(ctx context.Context) tool.InvocationResult {
			runtime.readObservations.invalidate(candidate.RelativePath())
			return runtime.writeExecutor.Execute(ctx, candidate)
		},
		matches: func(result tool.InvocationResult) bool {
			return result.Status() != tool.ResultSuccess || result.WriteMetadata() == candidate.ResultMetadata()
		},
	})
	return result, false, err
}

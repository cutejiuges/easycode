package runtime

import (
	"context"
	"errors"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/tool"
)

func (runtime *Runtime) executeWriteCall(ctx context.Context, emit Emitter, turnID domain.TurnID, invocation tool.Invocation) (tool.InvocationResult, bool, error) {
	v, ok := invocation.Write()
	if !ok || runtime.tools.PrepareWrite == nil || runtime.tools.Write == nil {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Write runtime is not configured")
	}
	finish := func(result tool.InvocationResult) (tool.InvocationResult, bool, error) {
		if err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result); err != nil {
			return tool.InvocationResult{}, false, err
		}
		return result, false, nil
	}
	if ctx.Err() != nil {
		return finish(tool.NewWriteErrorResult(v, tool.ResultCancelled, "cancelled", "Write cancelled"))
	}
	candidate, err := runtime.tools.PrepareWrite.Prepare(ctx, v, runtime.readObservations.lookup(v.Input().FilePath()))
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
	policy := runtime.tools.Policy.Decide(call)
	return runtime.executeWithApproval(ctx, emit, request, invocation, policy, "Write", exclusiveAction{
		execute: func(ctx context.Context) tool.InvocationResult {
			runtime.readObservations.invalidate(candidate.RelativePath())
			return runtime.tools.Write.Execute(ctx, candidate)
		},
		matches: func(result tool.InvocationResult) bool {
			return result.Status() != tool.ResultSuccess || result.WriteMetadata() == candidate.ResultMetadata()
		},
	})
}

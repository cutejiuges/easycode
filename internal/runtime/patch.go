package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/tool"
	"errors"
)

func (runtime *Runtime) executePatchCall(ctx context.Context, emit Emitter, turnID domain.TurnID, invocation tool.Invocation) (tool.InvocationResult, bool, error) {
	patch, ok := invocation.Patch()
	if !ok || runtime.tools.PreparePatch == nil || runtime.tools.Patch == nil {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Patch runtime is not configured")
	}
	candidate, err := runtime.tools.PreparePatch.Prepare(ctx, patch)
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
	policy := runtime.tools.Policy.Decide(callFromPatchInvocation(patch))
	return runtime.executeWithApproval(ctx, emit, request, invocation, policy, "Patch", exclusiveAction{
		execute: func(ctx context.Context) tool.InvocationResult {
			runtime.readObservations.invalidate(candidate.RelativePath())
			return runtime.tools.Patch.Execute(ctx, candidate)
		},
		matches: func(result tool.InvocationResult) bool {
			return result.Status() != tool.ResultSuccess || result.PatchMetadata() == candidate.ResultMetadata()
		},
	})
}

func callFromPatchInvocation(invocation tool.PatchInvocation) tool.ReadyCall {
	call, _ := tool.NewPatchReadyCall(invocation.ProviderCallID(), invocation.WireKind(), invocation.Input())
	return call
}

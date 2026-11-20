package runtime

import (
	"context"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func (runtime *Runtime) executeExecCall(ctx context.Context, emit Emitter, turnID domain.TurnID, invocation tool.Invocation) (tool.InvocationResult, bool, error) {
	execInvocation, ok := invocation.Exec()
	if !ok || runtime.execPreparer == nil || runtime.execExecutor == nil {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Exec runtime is not configured")
	}
	finish := func(result tool.InvocationResult, aborted bool) (tool.InvocationResult, bool, error) {
		if err := runtime.persistToolResult(context.WithoutCancel(ctx), turnID, &result); err != nil {
			return tool.InvocationResult{}, false, err
		}
		return result, aborted, nil
	}
	if ctx.Err() != nil {
		return finish(tool.NewExecErrorResult(execInvocation, tool.ResultCancelled, "cancelled", "Exec cancelled"), false)
	}
	candidate, err := runtime.execPreparer.Prepare(execInvocation)
	if err != nil {
		return finish(tool.NewExecErrorResult(execInvocation, tool.ResultError, "exec_prepare_failed", "Exec failed: exec_prepare_failed"), false)
	}
	if candidate.Validate() != nil || candidate.Invocation() != execInvocation {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Exec candidate does not match invocation")
	}
	allowance, err := runtime.resultBodyAllowance(invocation)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	candidate, err = candidate.WithPreviewAllowance(allowance)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	requestID, err := runtime.newApprovalRequestID()
	if err != nil {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "approval identity generation failed")
	}
	request, err := tool.NewExecApprovalRequest(requestID, runtime.sessionID, runtime.threadID, turnID, candidate)
	if err != nil {
		return tool.InvocationResult{}, false, fault.New(fault.CodeTurnFailed, "Exec approval is invalid")
	}
	outcome := tool.ApprovalOutcomeDeny
	feedback := ""
	source := session.ApprovalSourceHeadless
	if runtime.interactiveApprovals {
		source = session.ApprovalSourceInteractive
		response, waitErr := runtime.waitForApproval(ctx, emit, turnID, request)
		if waitErr != nil {
			return tool.InvocationResult{}, false, waitErr
		}
		feedback = response.Feedback()
		switch response.Decision() {
		case tool.ApprovalAllowOnce:
			outcome = tool.ApprovalOutcomeAllowOnce
		case tool.ApprovalAbort:
			outcome = tool.ApprovalOutcomeAbort
		}
	}
	if ctx.Err() != nil {
		outcome = tool.ApprovalOutcomeAbort
		feedback = ""
		source = session.ApprovalSourceInterruption
	}
	factDraft, err := buildApprovalFact(request, invocation, outcome, source, feedback)
	if err != nil {
		return tool.InvocationResult{}, false, err
	}
	if outcome != tool.ApprovalOutcomeAllowOnce {
		status, code := tool.ResultDenied, "approval_denied"
		if !runtime.interactiveApprovals {
			code = "approval_required"
		}
		if outcome == tool.ApprovalOutcomeAbort {
			status, code = tool.ResultCancelled, "user_aborted"
		}
		message := "Exec stopped: " + code
		if feedback != "" {
			message += "\nUser feedback: " + feedback
		}
		result := tool.NewExecErrorResult(execInvocation, status, code, message)
		return runtime.commitApprovalDenial(ctx, emit, request, outcome, factDraft, result, "Exec")
	}
	if err := runtime.commitApprovalDecision(ctx, factDraft, "Exec"); err != nil {
		return tool.InvocationResult{}, false, err
	}
	runtime.emitApprovalResolved(emit, turnID, request, outcome)
	summary := strings.Join(strings.Fields(execInvocation.Input().Command()), " ")
	runes := []rune(summary)
	if len(runes) > 160 {
		summary = string(runes[:159]) + "…"
	}
	activity := protocol.ToolActivityPayload{Tool: protocol.ToolShell, InvocationID: string(invocation.InvocationID()), Summary: summary, Workdir: execInvocation.Input().Workdir(), Status: "running"}

	result, err := runtime.executeExclusive(ctx, turnID, invocation, exclusiveAction{
		execute: func(ctx context.Context) tool.InvocationResult {
			return executeWithProgress(ctx, runtime.execExecutor, candidate, func(progress tool.ExecProgress) {
				payload := activity
				payload.ElapsedMillis = progress.ElapsedMillis()
				payload.StdoutBytes = progress.StdoutBytes()
				payload.StderrBytes = progress.StderrBytes()
				payload.StdoutLines = progress.StdoutLines()
				payload.StderrLines = progress.StderrLines()
				payload.Recent = progress.Recent()
				payload.Truncated = progress.Truncated()
				if event, err := protocol.NewToolProgress(payload); err == nil {
					emit(runtime.decorate(event, turnID))
				}
			})
		},
		matches: func(result tool.InvocationResult) bool {
			return result.ExecMetadata().Workdir() == candidate.Invocation().Input().Workdir()
		},
	})
	return result, false, err
}

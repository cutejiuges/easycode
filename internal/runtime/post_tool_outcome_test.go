package runtime

import (
	"context"
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type postFailedPatchPreparation struct{}

func (postFailedPatchPreparation) Prepare(context.Context, tool.PatchInvocation) (tool.PatchCandidate, error) {
	return tool.PatchCandidate{}, errors.New("fixture preparation failed")
}

type postFailedWritePreparation struct{}

func (postFailedWritePreparation) Prepare(context.Context, tool.WriteInvocation, tool.ReadObservation) (tool.WriteCandidate, error) {
	return tool.WriteCandidate{}, errors.New("fixture preparation failed")
}

type postFailedExecPreparation struct{}

func (postFailedExecPreparation) Prepare(tool.ExecInvocation) (tool.ExecCandidate, error) {
	return tool.ExecCandidate{}, errors.New("fixture preparation failed")
}

type postPatchOutcome struct{ status tool.ResultStatus }

func (e postPatchOutcome) Execute(_ context.Context, c tool.PatchCandidate) tool.InvocationResult {
	ready, _ := tool.NewPatchReadyCall(c.ProviderCallID(), c.WireKind(), c.Input())
	invocation, _ := tool.NewPatchInvocation(c.InvocationID(), ready)
	return tool.NewPatchErrorResult(invocation, e.status, "fixture_outcome", "Tool did not succeed")
}

func TestPostChecksNeverRunForUnsuccessfulMutationOutcomes(t *testing.T) {
	for _, capability := range []tool.CapabilityID{tool.CapabilityPatch, tool.CapabilityWrite, tool.CapabilityExec} {
		for _, mode := range []string{"prepare", "denied", "cancelled", "error", "conflict", "uncertain"} {
			// Exec 没有文件冲突状态；该状态只适用于两项文件 mutation。
			if capability == tool.CapabilityExec && mode == "conflict" {
				continue
			}
			t.Run(string(capability)+"/"+mode, func(t *testing.T) {
				j := &fakeJournal{}
				r, inv, runner, _, _ := postRuntimeFixture(t, j, domain.HookFeedbackModel)
				status := tool.ResultError
				switch mode {
				case "denied":
					status = tool.ResultDenied
				case "cancelled":
					status = tool.ResultCancelled
				case "conflict":
					status = tool.ResultConflict
				case "uncertain":
					status = tool.ResultOutcomeUncertain
				}
				switch capability {
				case tool.CapabilityPatch:
					r.patchExecutor = postPatchOutcome{status}
					if mode == "prepare" {
						r.patchPreparer = postFailedPatchPreparation{}
					}
				case tool.CapabilityWrite:
					var candidate tool.WriteCandidate
					inv, candidate = runtimeWriteInvocation(t)
					r.writePreparer = &fixedWritePreparer{candidate: candidate}
					r.writeExecutor = &fixedWriteExecutor{resultOverride: func(c tool.WriteCandidate) tool.InvocationResult {
						return tool.NewWriteCandidateResult(c, status, "fixture_outcome")
					}}
					if mode == "prepare" {
						r.writePreparer = postFailedWritePreparation{}
					}
				case tool.CapabilityExec:
					var components *fixedExecComponents
					_, inv, components = runtimeExecFixture(t, j)
					r.execPreparer, r.execExecutor = components, components
					components.resultOverride = func(c tool.ExecCandidate) tool.InvocationResult {
						return tool.NewExecErrorResult(c.Invocation(), status, "fixture_outcome", "Tool did not succeed")
					}
					if mode == "prepare" {
						r.execPreparer = postFailedExecPreparation{}
					}
				}
				approval := tool.ApprovalAllowOnce
				if mode == "denied" {
					approval = tool.ApprovalDeny
				}
				result, _, err := r.executeApproved(context.Background(), postApprovalEmitter(t, r, inv, protocol.HookTrustSession, approval), runtimeTurnID, inv)
				if result.Validate() != nil || result.Status() != status || runner.calls != 0 {
					t.Fatal("unsuccessful mutation ran post checks or changed status", err, result.Status(), runner.calls)
				}
				state := r.hooks.post[inv.InvocationID()]
				if mode == "uncertain" {
					if err == nil || !r.poisoned.Load() || state.terminal != nil {
						t.Fatal("uncertain effect crossed owner recovery barrier")
					}
				} else if err != nil || state.terminal == nil || state.terminal.Disposition != domain.PostHookSkipped || state.terminal.Code != domain.HookToolNotSuccessful || len(postFeedback(state)) != 0 || r.resultTicket != nil {
					t.Fatal("unsuccessful mutation did not close without disclosure", err)
				}
				count := 0
				for _, batch := range j.snapshot() {
					for _, draft := range batch {
						if draft.EventKind() == session.EventToolCallResult {
							count++
						}
					}
				}
				if count != 1 {
					t.Fatal("mutation result was not unique", count)
				}
			})
		}
	}
}

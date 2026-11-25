package testsupport

import (
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// PostResult 装配共享工具结果；两家测试仍各自提供实际原生调用。
func PostResult(t *testing.T, capability domain.HookCapability, patchWire tool.ProviderWireKind) tool.InvocationResult {
	t.Helper()
	id := tool.InvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	var result tool.InvocationResult
	var err error
	switch capability {
	case domain.HookPatch:
		replacement, _ := tool.NewPatchReplacement("old", "new", false)
		input, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
		ready, _ := tool.NewPatchReadyCall("call-post", patchWire, input)
		inv, _ := tool.NewPatchInvocation(id, ready)
		metadata, _ := tool.NewPatchResultMetadata("main.go", 1, 1, 1)
		preview, _ := tool.NewModelPreview("Updated main.go (1 replacements, +1 -1 lines)")
		result, err = tool.NewPatchInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
	case domain.HookWrite:
		input, _ := tool.NewWriteInput("written", "new")
		ready, _ := tool.NewWriteReadyCall("call-post", input)
		inv, _ := tool.NewWriteInvocation(id, ready)
		metadata, _ := tool.NewWriteResultMetadata("written", tool.WriteCreate, 3, 1, 0)
		preview, _ := tool.NewModelPreview("Created written (3 bytes, 1 lines, 0 directories created)")
		result, err = tool.NewWriteInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
	case domain.HookExec:
		input, _ := tool.NewExecInput("exit 7", "", 1000)
		ready, _ := tool.NewExecReadyCall("call-post", input)
		inv, _ := tool.NewExecInvocation(id, ready)
		metadata, _ := tool.NewExecResultMetadata("", 7, true, 1, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
		preview, _ := tool.NewModelPreview("Exec completed with exit 7")
		result, err = tool.NewExecInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
	}
	if err != nil || result.Validate() != nil {
		t.Fatal("invalid provider post fixture", err)
	}
	result, err = tool.FinalizeResult(result, 240<<10, tool.BudgetPreserved)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// PostOutputAtCut 装配冻结 Hook 结果，避免为相同证据链复制两套代码。
func PostOutputAtCut(t *testing.T, original tool.Invocation, result tool.InvocationResult, cut string) tool.ModelOutput {
	t.Helper()
	event := domain.HookPostToolUse
	if result.Status() == tool.ResultError || result.Status() == tool.ResultConflict {
		event = domain.HookPostToolUseFailure
	}
	d, err := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackModel, strings.Repeat("a", 64), "check", 7, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	refs := []domain.HookReference{d.Reference}
	if cut == "between" {
		next, err := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackModel, d.Reference.Revision, "next", 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, next.Reference)
	}
	p, _ := domain.NewHookPlan(string(result.InvocationID()), domain.HookCapability(result.Capability()), refs)
	currentPostBranch1, _ := domain.NewPostHookBranch(p.Event, p.Hooks, 16<<10)
	currentPostBranch1.Delivery = domain.HookDeliveryAvailable
	plan, _ := domain.NewPostHookPlan(p.InvocationID, p.Capability, []domain.PostHookBranch{currentPostBranch1}, 256<<10, 16<<10)
	id, _ := domain.NewPostHookRunIdentity(plan, event, 0)
	stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution", StdoutBytes: 100}
	candidate, _ := domain.NewHookFeedbackCandidate("Read-only check: keep TODO visible.")
	frozen, err := tool.FreezePostFeedback(plan, nil, stats, candidate)
	if err != nil {
		t.Fatal(err)
	}
	admission, _ := tool.NewExecutionAdmission(result.InvocationID(), 1)
	selection, err := tool.NewPostHookSelection(plan, admission, result)
	if err != nil {
		t.Fatal(err)
	}
	feedback := frozen.Feedback
	if cut == "cancelled" {
		feedback, err = domain.NewFrozenHookFeedback(id, d.Reference, domain.HookFeedbackCode, "", domain.HookCancelled, 1)
		if err != nil {
			t.Fatal(err)
		}
		text, err := tool.RenderPostFeedback([]domain.FrozenHookFeedback{feedback})
		if err != nil {
			t.Fatal(err)
		}
		feedback.BudgetDelta = len(text)
	}
	direct := DirectModelOutput(t, original, result, admission)
	output, err := tool.NewPostModelOutput(result, direct.Evidence(), selection, []domain.FrozenHookFeedback{feedback})
	if err != nil {
		t.Fatal(err)
	}
	return output
}

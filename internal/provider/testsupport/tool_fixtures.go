package testsupport

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func DirectModelOutput(t testing.TB, invocation tool.Invocation, result tool.InvocationResult, admission tool.ExecutionAdmission) tool.ModelOutput {
	t.Helper()
	const id = "01900000-0000-7000-8000-000000000001"
	origin, err := tool.NewToolCallOrigin(domain.SessionID(id), domain.ThreadID(id), domain.TurnID(id), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := tool.NewUnhookedInputSelection(origin, invocation)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := tool.NewInputResultEvidence(selection, admission, tool.InputResultExecution)
	if err != nil {
		t.Fatal(err)
	}
	output, err := tool.NewModelOutput(result, evidence)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func ReadOutput(t testing.TB, call, path, text string) tool.ModelOutput {
	t.Helper()
	input, err := tool.NewReadInput(path, 1, 2000)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := tool.NewReadReadyCall(tool.ProviderCallID(call), input)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	inv, err := tool.NewReadInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := tool.NewReadResultMetadata(path, input, 1, 1, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := tool.NewModelPreview(text)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.NewReadInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	return DirectModelOutput(t, invocation, result, tool.ExecutionAdmission{})
}
func RestoreFixtureOutputs(t *testing.T, prefix string, wire tool.ProviderWireKind) [][]tool.ModelOutput {
	t.Helper()
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	globInput, _ := tool.DecodeGlobInput([]byte(`{"pattern":"**/*.go"}`))
	globReady, _ := tool.NewGlobReadyCall(tool.ProviderCallID(prefix+"glob"), globInput)
	globInv, _ := tool.NewGlobInvocation(id, globReady)
	globMeta, _ := tool.NewGlobResultMetadata(nil, false, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	globPreview, _ := tool.NewModelPreview("frozen glob")
	glob, err := tool.NewGlobInvocationResult(globInv, tool.ResultSuccess, "ok", globPreview, globMeta)
	if err != nil {
		t.Fatal(err)
	}
	grepInput, _ := tool.DecodeGrepInput([]byte(`{"pattern":"TODO"}`))
	grepReady, _ := tool.NewGrepReadyCall(tool.ProviderCallID(prefix+"grep"), grepInput)
	grepInv, _ := tool.NewGrepInvocation(id, grepReady)
	grepMeta, _ := tool.NewGrepResultMetadata(tool.GrepOutputFilesWithMatches, nil, 0, false, 0, 0, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	grepPreview, _ := tool.NewModelPreview("frozen grep")
	grep, err := tool.NewGrepInvocationResult(grepInv, tool.ResultSuccess, "ok", grepPreview, grepMeta)
	if err != nil {
		t.Fatal(err)
	}
	replacement, _ := tool.NewPatchReplacement("old", "new", false)
	if wire == tool.WireCustomFreeform {
		replacement, _ = tool.NewPatchLineReplacement([]string{"old"}, []string{"new"})
	}
	patchInput, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
	patchReady, _ := tool.NewPatchReadyCall(tool.ProviderCallID(prefix+"edit"), wire, patchInput)
	patchInv, _ := tool.NewPatchInvocation(id, patchReady)
	patchMeta, _ := tool.NewPatchResultMetadata("main.go", 1, 1, 1)
	patchPreview, _ := tool.NewModelPreview("frozen patch")
	patch, err := tool.NewPatchInvocationResult(patchInv, tool.ResultSuccess, "ok", patchPreview, patchMeta)
	if err != nil {
		t.Fatal(err)
	}
	writeInput, _ := tool.NewWriteInput("new/a", "中文\r\n")
	writeReady, _ := tool.NewWriteReadyCall(tool.ProviderCallID(prefix+"write"), writeInput)
	writeInv, _ := tool.NewWriteInvocation(id, writeReady)
	writeMeta, _ := tool.NewWriteResultMetadata("new/a", tool.WriteCreate, 8, 1, 1)
	writePreview, _ := tool.NewModelPreview("Created new/a (8 bytes, 1 lines, 1 directories created)")
	write, err := tool.NewWriteInvocationResult(writeInv, tool.ResultSuccess, "ok", writePreview, writeMeta)
	if err != nil {
		t.Fatal(err)
	}
	read := ReadOutput(t, prefix+"read", "README.md", "frozen read")
	globInvocation, _ := tool.NewInvocation(id, globReady)
	grepInvocation, _ := tool.NewInvocation(id, grepReady)
	patchInvocation, _ := tool.NewInvocation(id, patchReady)
	writeInvocation, _ := tool.NewInvocation(id, writeReady)
	return [][]tool.ModelOutput{{DirectModelOutput(t, globInvocation, glob, tool.ExecutionAdmission{}), DirectModelOutput(t, grepInvocation, grep, tool.ExecutionAdmission{}), read}, {DirectModelOutput(t, patchInvocation, patch, tool.ExecutionAdmission{})}, {DirectModelOutput(t, writeInvocation, write, tool.ExecutionAdmission{})}}
}

func FailureResult(t *testing.T, r tool.InvocationResult) tool.InvocationResult {
	return FailureResultWithCode(t, r, "execution_failed")
}
func FailureResultWithCode(t *testing.T, r tool.InvocationResult, code string) tool.InvocationResult {
	t.Helper()
	var result tool.InvocationResult
	switch r.Capability() {
	case tool.CapabilityPatch:
		replacement, _ := tool.NewPatchReplacement("old", "new", false)
		input, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
		ready, _ := tool.NewPatchReadyCall(r.ProviderCallID(), r.WireKind(), input)
		v, _ := tool.NewPatchInvocation(r.InvocationID(), ready)
		result = tool.NewPatchErrorResult(v, tool.ResultError, code, "Tool execution failed")
	case tool.CapabilityWrite:
		input, _ := tool.NewWriteInput("written", "new")
		ready, _ := tool.NewWriteReadyCall(r.ProviderCallID(), input)
		v, _ := tool.NewWriteInvocation(r.InvocationID(), ready)
		result = tool.NewWriteErrorResult(v, tool.ResultError, code, "Tool execution failed")
	case tool.CapabilityExec:
		input, _ := tool.NewExecInput("exit 7", "", 1000)
		ready, _ := tool.NewExecReadyCall(r.ProviderCallID(), input)
		v, _ := tool.NewExecInvocation(r.InvocationID(), ready)
		result = tool.NewExecErrorResult(v, tool.ResultError, code, "Tool execution failed")
	}
	result, err := tool.FinalizeResult(result, 240<<10, tool.BudgetPreserved)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func RewriteOutput(t *testing.T, original tool.Invocation, mode string) tool.ModelOutput {
	t.Helper()
	before, err := tool.HookInputForInvocation(original)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := tool.HookProfileForInvocation(original)
	if err != nil {
		t.Fatal(err)
	}
	var next domain.HookToolInput
	switch original.Capability() {
	case tool.CapabilityPatch:
		line := profile == domain.HookLinePatch
		old, text := "old", "rewritten"
		if line {
			old += "\n"
			text += "\n"
		}
		next, _ = domain.NewHookPatchInput("effective.txt", []domain.HookReplacement{{OldText: old, NewText: text, LineBased: line}})
	case tool.CapabilityWrite:
		next, _ = domain.NewHookWriteInput("effective.txt", "rewritten\n")
	case tool.CapabilityExec:
		next, _ = domain.NewHookExecInput("printf rewritten; exit 7", "", 2000)
	}
	refs := make([]domain.HookReference, 1)
	declaration, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, domain.HookDigest(nil), "rewrite", 7, "true", 5)
	refs[0] = declaration.Reference
	if mode == "revert" || mode == "rejected" {
		second := declaration.Reference
		second.ID = "last"
		refs = append(refs, second)
	}
	plan, err := domain.NewHookPlan(string(original.InvocationID()), domain.HookCapability(original.Capability()), refs)
	if err != nil {
		t.Fatal(err)
	}
	current := before
	var finished []domain.HookFinished
	for i, ref := range refs {
		after := next
		if mode == "unchanged" || mode == "revert" && i == 1 {
			after = before
		}
		effect, err := domain.NewUnchangedHookInputEffect(current)
		if err != nil {
			t.Fatal(err)
		}
		if !current.Equal(after) {
			effect, err = domain.NewReplacedHookInputEffect(current, after, profile)
			if err != nil {
				t.Fatal(err)
			}
		}
		id, _ := domain.NewHookRunIdentity(plan, i)
		stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
		if mode == "rejected" && i == 1 {
			stats.Status = domain.HookBlock
			stats.Code = domain.HookDenied
			stats.ExitCode = 2
			effect, _ = domain.NewUnchangedHookInputEffect(current)
		} else {
			current = after
		}
		fact, err := domain.NewPreHookFinished(stats, ref, effect)
		if err != nil {
			t.Fatal(err)
		}
		finished = append(finished, fact)
	}
	const identity = "01900000-0000-7000-8000-000000000001"
	origin, _ := tool.NewToolCallOrigin(domain.SessionID(identity), domain.ThreadID(identity), domain.TurnID(identity), 3, 2)
	status, code, blocking := domain.HookContinue, domain.HookOK, -1
	if mode == "rejected" {
		status, code, blocking = domain.HookBlock, domain.HookDenied, 1
	}
	decision, _ := domain.NewHookGateDecision(plan, status, code, len(finished), blocking)
	selection, err := tool.NewSelectedInputSelection(origin, original, plan, finished, decision)
	if mode == "rejected" {
		selection, err = tool.NewRejectedInputSelection(origin, original, plan, finished, decision)
	}
	if err != nil {
		t.Fatal(err)
	}
	effective, err := selection.ResultInvocation()
	if err != nil {
		t.Fatal(err)
	}
	var result tool.InvocationResult
	preview, _ := tool.NewModelPreview("frozen result with literal [PreToolUse input rewrite] label")
	switch effective.Capability() {
	case tool.CapabilityPatch:
		v, _ := effective.Patch()
		m, _ := tool.NewPatchResultMetadata(v.Input().FilePath(), len(v.Input().Replacements()), 1, 1)
		result, err = tool.NewPatchInvocationResult(v, tool.ResultSuccess, "ok", preview, m)
	case tool.CapabilityWrite:
		v, _ := effective.Write()
		m, _ := tool.NewWriteResultMetadata(v.Input().FilePath(), tool.WriteCreate, len(v.Input().Content()), tool.WriteLineCount(v.Input().Content()), 0)
		result, err = tool.NewWriteInvocationResult(v, tool.ResultSuccess, "ok", preview, m)
	case tool.CapabilityExec:
		v, _ := effective.Exec()
		m, _ := tool.NewExecResultMetadata(v.Input().Workdir(), 7, true, 1, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
		result, err = tool.NewExecInvocationResult(v, tool.ResultSuccess, "ok", preview, m)
	}
	if err != nil {
		t.Fatal(err)
	}
	var admission tool.ExecutionAdmission
	if mode != "rejected" && mode != "denied" && mode != "recovery-unadmitted" {
		admission, _ = tool.NewExecutionAdmission(original.InvocationID(), 90)
	}
	resultOrigin := tool.InputResultExecution
	reason, allowance := tool.BudgetPreserved, tool.MaxModelPreviewBytes
	switch mode {
	case "rejected":
		result = tool.NewInvocationErrorResult(original, tool.ResultDenied, string(domain.HookDenied), "Check rejected candidate")
	case "denied":
		result = tool.NewInvocationErrorResult(effective, tool.ResultDenied, "user_denied", "Tool denied")
	case "start-failed":
		result = tool.NewInvocationErrorResult(effective, tool.ResultError, "process_start_failed", "Tool process could not start")
	case "recovery-unadmitted":
		result = tool.NewInvocationErrorResult(effective, tool.ResultCancelled, "session_interrupted_before_execution", "Tool was not rerun")
		resultOrigin = tool.InputResultRecovery
		reason = tool.BudgetRecovery
		allowance = tool.SafetyResultBytes
	case "recovery-admitted":
		result = tool.NewInvocationErrorResult(effective, tool.ResultOutcomeUncertain, "outcome_uncertain", "Tool outcome is uncertain")
		resultOrigin = tool.InputResultRecovery
		reason = tool.BudgetRecovery
		allowance = tool.SafetyResultBytes
	}
	evidence, err := tool.NewInputResultEvidence(selection, admission, resultOrigin)
	if err != nil {
		t.Fatal(err)
	}
	result, err = tool.FreezeInputRewriteResult(result, evidence, allowance, reason)
	if err != nil {
		t.Fatal(err)
	}
	output, err := tool.NewModelOutput(result, evidence)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func FileInput(t *testing.T, skill bool) domain.UserInput {
	t.Helper()
	selection, _ := domain.NewLineFileSelection(2, 8)
	ref, _ := domain.NewFileReference("src/a.go", selection)
	a, err := domain.FreezeFileAttachment(ref, "first\r\nsecond\nlast")
	if err != nil {
		t.Fatal(err)
	}
	ref, _ = domain.NewFileReference("docs/a b.md", domain.NewWholeFileSelection())
	b, err := domain.FreezeFileAttachment(ref, "\ufeff@not-recursive\r\n<safe>&\nend")
	if err != nil {
		t.Fatal(err)
	}
	files, err := domain.NewFileAttachments([]domain.FileAttachment{a, b})
	if err != nil {
		t.Fatal(err)
	}
	raw := `inspect @src/a.go#L2-8 and @"docs/a b.md"`
	text := raw
	if skill {
		text = "$qa:demo " + text
	}
	u, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000011", text)
	u, err = u.WithFileAttachments(files)
	if err != nil {
		t.Fatal(err)
	}
	if skill {
		source, _ := domain.NewSkillSource("plugin", "qa", "codex")
		attachment, _ := domain.NewSkillAttachment(source, "qa:demo", domain.SkillDigest("source"), raw, "Frozen skill instructions")
		u, err = u.WithSkillAttachment(attachment)
		if err != nil {
			t.Fatal(err)
		}
	}
	return u
}

package tool

import (
	"testing"

	"easycode/internal/domain"
)

func inputSelectionOrigin(t *testing.T) ToolCallOrigin {
	t.Helper()
	const id = "018f1d8a-7b5c-7def-8123-456789abcdef"
	v, err := NewToolCallOrigin(domain.SessionID(id), domain.ThreadID(id), domain.TurnID(id), 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func directInputResultEvidence(t *testing.T, invocation Invocation, admission ExecutionAdmission) InputResultEvidence {
	t.Helper()
	selection, err := NewUnhookedInputSelection(inputSelectionOrigin(t), invocation)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := NewInputResultEvidence(selection, admission, InputResultExecution)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
func rewriteExecInvocation(t *testing.T) Invocation {
	t.Helper()
	input, _ := NewExecInput("original", "", 1000)
	call, _ := NewExecReadyCall("call", input)
	inv, err := NewInvocation("018f1d8a-7b5c-7def-8123-456789abcdef", call)
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestInputSelectionKeepsAllNineDirectCalls(t *testing.T) {
	var calls []ReadyCall
	add := func(call ReadyCall, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	read, _ := NewReadInput("f", 1, 10)
	add(NewReadReadyCall("call", read))
	glob, _ := NewGlobInput("**/*.go", "", 10)
	add(NewGlobReadyCall("call", glob))
	grep, _ := NewGrepInput("x", "", "", GrepOutputContent, false, 0, 0, 10)
	add(NewGrepReadyCall("call", grep))
	r, _ := NewPatchReplacement("a", "b", false)
	patch, _ := NewPatchInput("f", []PatchReplacement{r})
	add(NewPatchReadyCall("call", WireStructured, patch))
	write, _ := NewWriteInput("f", "")
	add(NewWriteReadyCall("call", write))
	exec, _ := NewExecInput("echo x", "", 1000)
	add(NewExecReadyCall("call", exec))
	artifact, _ := DecodeReadArtifactInput([]byte(`{"artifact_id":"0123456789abcdef0123456789abcdef"}`))
	add(NewReadArtifactReadyCall("call", artifact))
	skill, _ := NewSkillLoadInput("demo", "")
	add(NewSkillReadyCall("call", skill))
	question := questionInvocationFixture(t)
	add(NewQuestionReadyCall("call", question.Input()))
	for _, call := range calls {
		t.Run(string(call.Capability()), func(t *testing.T) {
			inv, err := NewInvocation("018f1d8a-7b5c-7def-8123-456789abcdef", call)
			if err != nil {
				t.Fatal(err)
			}
			selection, err := NewUnhookedInputSelection(inputSelectionOrigin(t), inv)
			if err != nil {
				t.Fatal(err)
			}
			effective, ok := selection.EffectiveInvocation()
			if !ok || !effective.Equal(inv) || selection.Changed() || selection.Kind() != InputUnhooked {
				t.Fatal("direct input changed")
			}
			ready, err := effective.ReadyCall()
			again, againErr := NewInvocation(inv.InvocationID(), ready)
			if err != nil || againErr != nil || !again.Equal(inv) {
				t.Fatal("strategy input lost fields")
			}
		})
	}
}

func TestRewriteInvocationRetainsIdentityAndFacadeProfiles(t *testing.T) {
	for _, wire := range []ProviderWireKind{WireStructured, WireCustomFreeform} {
		r, _ := NewPatchReplacement("old", "new", false)
		if wire == WireCustomFreeform {
			r, _ = NewPatchLineReplacement([]string{"old"}, []string{"new"})
		}
		p, _ := NewPatchInput("original.txt", []PatchReplacement{r})
		ready, _ := NewPatchReadyCall("call", wire, p)
		inv, _ := NewInvocation("018f1d8a-7b5c-7def-8123-456789abcdef", ready)
		normalized, err := HookInputForInvocation(inv)
		profile, _ := HookProfileForInvocation(inv)
		if err != nil || normalized.ValidateProfile(profile) != nil {
			t.Fatal("invalid projection", err)
		}
		file, rs, _ := normalized.Patch()
		for _, mode := range []string{"target", "mode", "multiple", "replace-all", "cr"} {
			altered := append([]domain.HookReplacement(nil), rs...)
			switch mode {
			case "target":
				file = "rewritten.txt"
			case "mode":
				altered[0].LineBased = !altered[0].LineBased
			case "multiple":
				altered = append(altered, domain.HookReplacement{OldText: "c", NewText: "d"})
			case "replace-all":
				altered[0].ReplaceAll = true
			case "cr":
				altered[0].OldText = "old\r\n"
			}
			input, _ := domain.NewHookPatchInput(file, altered)
			effective, rewriteErr := RewriteInvocation(inv, input)
			valid := mode == "target" || wire == WireStructured && (mode == "replace-all" || mode == "cr")
			if (rewriteErr == nil) != valid {
				t.Fatalf("wire=%s mode=%s err=%v", wire, mode, rewriteErr)
			}
			if valid && (effective.InvocationID() != inv.InvocationID() || effective.ProviderCallID() != inv.ProviderCallID() || effective.WireKind() != wire || effective.Capability() != CapabilityPatch) {
				t.Fatal("identity changed")
			}
		}
		if inv.patch.input.FilePath() != "original.txt" {
			t.Fatal("original changed")
		}
	}
	write, _ := NewWriteInput("original.txt", "original")
	ready, _ := NewWriteReadyCall("call", write)
	inv, _ := NewInvocation("018f1d8a-7b5c-7def-8123-456789abcdef", ready)
	after, _ := domain.NewHookWriteInput("rewritten.txt", "")
	effective, err := RewriteInvocation(inv, after)
	if err != nil || effective.write.input.Content() != "" || effective.write.input.FilePath() != "rewritten.txt" || inv.write.input.Content() != "original" {
		t.Fatal("write replacement failed", err)
	}
	command, _ := domain.NewHookExecInput("rewritten", "subdir", 5000)
	effective, err = RewriteInvocation(rewriteExecInvocation(t), command)
	if err != nil || effective.exec.input.Command() != "rewritten" || effective.exec.input.Workdir() != "subdir" || effective.exec.input.TimeoutMillis() != 5000 {
		t.Fatal("exec replacement failed", err)
	}
	if _, err := RewriteInvocation(inv, command); err == nil {
		t.Fatal("cross capability accepted")
	}
}

func selectedExecFixture(t *testing.T, reject, revert bool) InputSelection {
	t.Helper()
	original := rewriteExecInvocation(t)
	var refs []domain.HookReference
	for i, source := range []domain.HookSource{domain.HookSourceUser, domain.HookSourceProject} {
		d, err := domain.NewHookDeclaration(source, domain.HookPreToolUse, domain.HookFeedbackNone, domain.HookDigest(nil), []string{"first", "second"}[i], 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, d.Reference)
	}
	plan, _ := domain.NewHookPlan(string(original.InvocationID()), domain.HookExec, refs)
	input, _ := HookInputForInvocation(original)
	changed, _ := domain.NewHookExecInput("rewritten", "subdir", 5000)
	var finished []domain.HookFinished
	for i := range refs {
		id, _ := domain.NewHookRunIdentity(plan, i)
		stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
		effect, _ := domain.NewUnchangedHookInputEffect(changed)
		if i == 0 {
			effect, _ = domain.NewReplacedHookInputEffect(input, changed, domain.HookExecProfile)
		}
		if i == 1 && revert {
			effect, _ = domain.NewReplacedHookInputEffect(changed, input, domain.HookExecProfile)
		}
		if i == 1 && reject {
			stats.Status, stats.Code, stats.ExitCode = domain.HookBlock, domain.HookDenied, 2
		}
		f, err := domain.NewPreHookFinished(stats, refs[i], effect)
		if err != nil {
			t.Fatal(err)
		}
		finished = append(finished, f)
	}
	status, code, blocking := domain.HookContinue, domain.HookOK, -1
	if reject {
		status, code, blocking = domain.HookBlock, domain.HookDenied, 1
	}
	decision, _ := domain.NewHookGateDecision(plan, status, code, 2, blocking)
	var selection InputSelection
	var err error
	if reject {
		selection, err = NewRejectedInputSelection(inputSelectionOrigin(t), original, plan, finished, decision)
	} else {
		selection, err = NewSelectedInputSelection(inputSelectionOrigin(t), original, plan, finished, decision)
	}
	if err != nil {
		t.Fatal(err)
	}
	return selection
}

func TestInputSelectionSeparatesCheckingRejectionAndFinalSelection(t *testing.T) {
	selected := selectedExecFixture(t, false, false)
	effective, ok := selected.EffectiveInvocation()
	if !ok || !selected.Changed() || effective.exec.input.Command() != "rewritten" || selected.Original().exec.input.Command() != "original" {
		t.Fatal("lost original or selected input")
	}
	rejected := selectedExecFixture(t, true, false)
	if _, ok := rejected.EffectiveInvocation(); ok {
		t.Fatal("rejected candidate executable")
	}
	resultInvocation, err := rejected.ResultInvocation()
	if err != nil || !resultInvocation.Equal(rejected.Original()) {
		t.Fatal("rejection used intermediate metadata")
	}
	reverted := selectedExecFixture(t, false, true)
	if reverted.Kind() != InputGateSelected || reverted.Changed() || len(reverted.Finished()) != 2 {
		t.Fatal("revert discarded gate evidence")
	}
	bad := selected.Clone()
	bad.origin = ToolCallOrigin{}
	if bad.Validate() == nil {
		t.Fatal("missing source accepted")
	}
	bad = selected.Clone()
	bad.original.exec.input.command = "forged"
	if bad.Validate() == nil {
		t.Fatal("same ID with different original accepted")
	}
	bad = selected.Clone()
	bad.finished[0].Feedback.Reference.ID = "forged"
	if bad.Validate() == nil {
		t.Fatal("wrong source accepted")
	}
	copy := selected.Finished()
	copy[0].Result.Code = domain.HookOutputInvalid
	if selected.Validate() != nil {
		t.Fatal("selection shared mutable facts")
	}
}

package tool

import (
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestInputRewriteResultUsesActualAdmissionAndOrigin(t *testing.T) {
	selection := selectedExecFixture(t, false, false)
	effective, _ := selection.EffectiveInvocation()
	admitted, _ := NewExecutionAdmission(effective.InvocationID(), 99)
	for _, test := range []struct {
		name      string
		admission ExecutionAdmission
		origin    InputResultOrigin
		status    ResultStatus
		code      string
		full      bool
	}{
		{"denied", ExecutionAdmission{}, InputResultExecution, ResultDenied, "approval_denied", false},
		{"post trust", ExecutionAdmission{}, InputResultExecution, ResultDenied, "hook_trust_denied", false},
		{"cancelled", ExecutionAdmission{}, InputResultExecution, ResultCancelled, "cancelled_before_execution", false},
		{"startup failed", admitted, InputResultExecution, ResultError, "exec_start_failed", true},
		{"uncertain execution", admitted, InputResultExecution, ResultOutcomeUncertain, "invalid_tool_result", true},
		{"recovery", admitted, InputResultRecovery, ResultOutcomeUncertain, "outcome_uncertain", false},
		{"unstarted recovery", ExecutionAdmission{}, InputResultRecovery, ResultCancelled, "session_interrupted_before_approval", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := NewInputResultEvidence(selection, test.admission, test.origin)
			if err != nil {
				t.Fatal(err)
			}
			result := NewInvocationErrorResult(effective, test.status, test.code, "Actual terminal: "+test.code)
			reason := BudgetPreserved
			if test.origin == InputResultRecovery {
				reason = BudgetRecovery
			}
			allowance := MaxModelPreviewBytes
			if test.admission.Sequence() == 0 || test.origin == InputResultRecovery {
				allowance = SafetyResultBytes
			}
			frozen, err := FreezeInputRewriteResult(result, evidence, allowance, reason)
			if err != nil {
				t.Fatal(err)
			}
			text := frozen.Preview().Text()
			if frozen.Status() != test.status || frozen.Code() != test.code || evidence.ValidateResult(frozen) != nil || frozen.Budget().ActualBytes() != len(text) {
				t.Fatal("terminal or budget changed")
			}
			if strings.Contains(text, `"command":"rewritten"`) != test.full || strings.Contains(text, `"workdir":"subdir"`) != test.full || strings.Contains(text, `"command":"original"`) {
				t.Fatal("wrong disclosed input", text)
			}
			if test.full && (!strings.Contains(text, "does not confirm process startup") || !strings.Contains(text, `"source":"user","id":"first"`)) {
				t.Fatal("missing truthful admission or source", text)
			}
			if !test.full {
				note, _ := inputRewriteNote(evidence, test.status)
				if len(note) > MaxRewriteShortNoteBytes || !strings.Contains(note, "input_digest=") {
					t.Fatal("unbounded safety note")
				}
			}
		})
	}
}

func TestInputRewriteBudgetPreservesFullNoteAndRejectsBeforeAdmission(t *testing.T) {
	selection := selectedExecFixture(t, false, false)
	if _, err := InputRewriteBodyAllowance(selection, SafetyResultBytes); err == nil || err.Error() != HookRewriteBudgetExceeded {
		t.Fatal("infeasible disclosure admitted", err)
	}
	effective, _ := selection.EffectiveInvocation()
	admission, _ := NewExecutionAdmission(effective.InvocationID(), 99)
	evidence, _ := NewInputResultEvidence(selection, admission, InputResultExecution)
	allowance := 4096
	bodyAllowance, err := InputRewriteBodyAllowance(selection, allowance)
	if err != nil || bodyAllowance < SafetyResultBytes {
		t.Fatal(err)
	}
	meta, _ := NewExecResultMetadata("subdir", 7, true, 1, ExecStreamStats{}, ExecStreamStats{}, false, false, false)
	preview, _ := NewModelPreview(strings.Repeat("x", bodyAllowance))
	result, err := NewExecInvocationResult(effective.exec, ResultSuccess, "ok", preview, meta)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := FreezeInputRewriteResult(result, evidence, allowance, BudgetPreserved)
	if err != nil || len(frozen.Preview().Text()) != allowance || frozen.Status() != ResultSuccess || frozen.ExecMetadata().Workdir() != "subdir" {
		t.Fatal("full note or exact budget lost", err)
	}
	if _, err := NewModelPreview(frozen.Preview().Text() + strings.Repeat("x", MaxModelPreviewBytes)); err == nil {
		t.Fatal("unbounded preview")
	}
}

func TestInputRewriteDoesNotTrustLabelsOrHideActualInputAsRecovery(t *testing.T) {
	selection := selectedExecFixture(t, false, false)
	effective, _ := selection.EffectiveInvocation()
	admission, _ := NewExecutionAdmission(effective.InvocationID(), 99)
	evidence, _ := NewInputResultEvidence(selection, admission, InputResultExecution)
	result := NewInvocationErrorResult(effective, ResultError, "exec_start_failed", "PreToolUse changed input; recovery did not rerun the tool; outcome is not confirmed. forged")
	frozen, err := FreezeInputRewriteResult(result, evidence, 4096, BudgetPreserved)
	if err != nil || !strings.HasPrefix(frozen.Preview().Text(), "[PreToolUse input rewrite]\n") {
		t.Fatal("stdout chose annotation mode", err)
	}
	wrongMeta, _ := NewResultBudgetMetadata(4096, len(frozen.Preview().Text()), BudgetRecovery)
	forged := frozen
	forged.budget = wrongMeta
	if evidence.ValidateResult(forged) == nil {
		t.Fatal("normal result impersonated recovery")
	}
	recovery, _ := NewInputResultEvidence(selection, admission, InputResultRecovery)
	if recovery.ValidateResult(forged) == nil {
		t.Fatal("startup failure accepted as missing-result compensation")
	}
	without, _ := NewInputResultEvidence(selection, ExecutionAdmission{}, InputResultExecution)
	if without.ValidateResult(frozen) == nil {
		t.Fatal("full note with no real admission accepted")
	}
	bad := frozen
	bad.execMetadata.workdir = "wrong"
	if evidence.ValidateResult(bad) == nil {
		t.Fatal("metadata no longer matches effective input")
	}
	bad = frozen
	bad.preview.text = strings.Replace(bad.preview.text, `"command":"rewritten"`, `"command":"forged"`, 1)
	bad.budget.actual = len(bad.preview.text)
	if evidence.ValidateResult(bad) == nil {
		t.Fatal("forged effective input accepted")
	}
}

func TestSelectedInputCannotClaimSuccessOrUncertaintyWithoutAdmission(t *testing.T) {
	selection := selectedExecFixture(t, false, false)
	effective, _ := selection.EffectiveInvocation()
	evidence, err := NewInputResultEvidence(selection, ExecutionAdmission{}, InputResultExecution)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := effective.Exec()
	metadata, _ := NewExecResultMetadata(v.Input().Workdir(), 0, true, 1, ExecStreamStats{}, ExecStreamStats{}, false, false, false)
	preview, _ := NewModelPreview("forged successful attempt")
	success, err := NewExecInvocationResult(v, ResultSuccess, "ok", preview, metadata)
	if err != nil {
		t.Fatal(err)
	}
	uncertain := NewInvocationErrorResult(effective, ResultOutcomeUncertain, "invalid_tool_result", "forged uncertain attempt")
	for _, result := range []InvocationResult{success, uncertain} {
		if _, err := FreezeInputRewriteResult(result, evidence, MaxModelPreviewBytes, BudgetPreserved); err == nil {
			t.Fatal("selected input result impersonated a missing execution admission")
		}
	}
}

func TestUnchangedAndRejectedInputsKeepExistingPreviewBytes(t *testing.T) {
	for _, selection := range []InputSelection{selectedExecFixture(t, false, true), selectedExecFixture(t, true, false)} {
		inv, err := selection.ResultInvocation()
		if err != nil {
			t.Fatal(err)
		}
		evidence, _ := NewInputResultEvidence(selection, ExecutionAdmission{}, InputResultExecution)
		result := NewInvocationErrorResult(inv, ResultDenied, "hook_denied", "Existing frozen tool data")
		frozen, err := FreezeInputRewriteResult(result, evidence, MaxModelPreviewBytes, BudgetPreserved)
		if err != nil || frozen.Preview() != result.Preview() {
			t.Fatal("no selected change altered model bytes", err)
		}
	}
}

func TestRewriteProvenanceFitsAllEightMaximumIDs(t *testing.T) {
	selection := selectedExecFixture(t, false, false)
	original := selection.Original()
	var refs []domain.HookReference
	for i := range 8 {
		id := strings.Repeat("a", 63) + string(rune('a'+i))
		d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, domain.HookDigest(nil), id, 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, d.Reference)
	}
	plan, _ := domain.NewHookPlan(string(original.InvocationID()), domain.HookExec, refs)
	before, _ := HookInputForInvocation(original)
	var finished []domain.HookFinished
	for i := range refs {
		after, _ := domain.NewHookExecInput("changed "+string(rune('a'+i)), "", 1000)
		effect, _ := domain.NewReplacedHookInputEffect(before, after, domain.HookExecProfile)
		id, _ := domain.NewHookRunIdentity(plan, i)
		f, err := domain.NewPreHookFinished(domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, refs[i], effect)
		if err != nil {
			t.Fatal(err)
		}
		finished = append(finished, f)
		before = after
	}
	decision, _ := domain.NewHookGateDecision(plan, domain.HookContinue, domain.HookOK, 8, -1)
	selected, err := NewSelectedInputSelection(inputSelectionOrigin(t), original, plan, finished, decision)
	if err != nil {
		t.Fatal(err)
	}
	note, err := fullInputRewriteNote(selected)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if !strings.Contains(note, ref.ID) {
			t.Fatal("source truncated")
		}
	}
}

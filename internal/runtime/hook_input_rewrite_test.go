package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func rewriteCandidateFixture(t *testing.T, r *Runtime, effective tool.Invocation) (func() int, func() int, string) {
	t.Helper()
	switch effective.Capability() {
	case tool.CapabilityPatch:
		v, _ := effective.Patch()
		input := v.Input()
		rs := input.Replacements()
		base, final := []byte(rs[0].OldText()), []byte(rs[0].NewText())
		evidence, _ := tool.NewFileBaseEvidence(tool.NewContentDigest(base), int64(len(base)), 0o644, 1, 2)
		metadata, _ := tool.NewPatchResultMetadata(input.FilePath(), len(rs), 1, 1)
		fingerprint, _ := tool.PatchFingerprint(input, evidence, final)
		candidate, err := tool.NewPatchCandidate(v, evidence, final, "--- old\n+++ rewritten\n", fingerprint, metadata)
		if err != nil {
			t.Fatal(err)
		}
		p, e := &fixedPatchPreparer{candidate: candidate}, &fixedPatchExecutor{}
		r.patchPreparer, r.patchExecutor = p, e
		return func() int { return p.calls }, func() int { return e.calls }, string(candidate.Fingerprint())
	case tool.CapabilityWrite:
		v, _ := effective.Write()
		root, _ := tool.NewDirectoryEvidence(".", 1, 2, 0o700)
		candidate, err := tool.NewWriteCandidate(v, tool.WriteCreate, tool.FileBaseEvidence{}, []tool.DirectoryEvidence{root}, nil, "+ rewritten\n")
		if err != nil {
			t.Fatal(err)
		}
		p, e := &fixedWritePreparer{candidate: candidate}, &fixedWriteExecutor{}
		r.writePreparer, r.writeExecutor = p, e
		return func() int { return p.calls }, func() int { return e.calls }, string(candidate.Fingerprint())
	case tool.CapabilityExec:
		v, _ := effective.Exec()
		shell, _ := tool.NewShellProfile("sh", "/bin/sh")
		environment, _ := tool.NewEnvironmentPolicy("minimal", []string{"PATH"})
		sandbox, _ := tool.NewSandboxProfile("workspace", false, true)
		candidate, err := tool.NewExecCandidate(v, shell, environment, sandbox)
		if err != nil {
			t.Fatal(err)
		}
		e := &fixedExecComponents{candidate: candidate}
		p := &rewriteExecPreparer{candidate: candidate}
		r.execPreparer, r.execExecutor = p, e
		return func() int { return p.calls }, func() int { return e.calls }, string(candidate.Fingerprint())
	default:
		t.Fatal("unsupported fixture capability")
		return nil, nil, ""
	}
}

type rewriteExecPreparer struct {
	candidate tool.ExecCandidate
	calls     int
}

func (p *rewriteExecPreparer) Prepare(tool.ExecInvocation) (tool.ExecCandidate, error) {
	p.calls++
	return p.candidate, nil
}

func rewrittenFixtureInput(t *testing.T, original tool.Invocation, second bool) domain.HookToolInput {
	t.Helper()
	target, content := "rewritten.txt", "rewritten"
	if second {
		target, content = "final.txt", "final"
	}
	var input domain.HookToolInput
	var err error
	switch original.Capability() {
	case tool.CapabilityPatch:
		input, err = domain.NewHookPatchInput(target, []domain.HookReplacement{{OldText: "old\n", NewText: content + "\n", LineBased: true}})
	case tool.CapabilityWrite:
		input, err = domain.NewHookWriteInput(target, content)
	case tool.CapabilityExec:
		input, err = domain.NewHookExecInput("printf "+content, "subdir", 5000)
	}
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func TestPreRewriteThreeCapabilitiesUseOnlySelectedInput(t *testing.T) {
	for _, capability := range []tool.CapabilityID{tool.CapabilityPatch, tool.CapabilityWrite, tool.CapabilityExec} {
		for _, mode := range []string{"rewrite-then-noop", "twice", "revert", "reject"} {
			for _, withPost := range []bool{false, true} {
				name := string(capability) + "/" + mode
				if withPost {
					name += "/post"
				}
				t.Run(name, func(t *testing.T) {
					j := &fakeJournal{}
					r, original, _, _ := hookRuntimeFixture(t, j, 0)
					if capability == tool.CapabilityWrite {
						original, _ = runtimeWriteInvocation(t)
					}
					if capability == tool.CapabilityExec {
						_, original, _ = runtimeExecFixture(t, j)
					}
					resetRuntimeReadyFixture(t, r, runtimeTurnID, original)
					r.resultBudget = tool.NewResultBudget()
					if err := r.resultBudget.BeginSample(1); err != nil {
						t.Fatal(err)
					}
					before, _ := tool.HookInputForInvocation(original)
					middle := rewrittenFixtureInput(t, original, false)
					final := middle
					if mode == "twice" {
						final = rewrittenFixtureInput(t, original, true)
					}
					if mode == "revert" {
						final = before
					}
					effective := original
					if !final.Equal(before) {
						var err error
						effective, err = tool.RewriteInvocation(original, final)
						if err != nil {
							t.Fatal(err)
						}
					}
					preparations, executions, fingerprint := rewriteCandidateFixture(t, r, effective)
					originalCandidates := &Runtime{}
					_, _, originalFingerprint := rewriteCandidateFixture(t, originalCandidates, original)
					var ds []domain.HookDeclaration
					for i, source := range []domain.HookSource{domain.HookSourceUser, domain.HookSourceProject} {
						d, err := domain.NewHookDeclaration(source, domain.HookPreToolUse, domain.HookFeedbackNone, domain.HookDigest(nil), []string{"first", "last"}[i], 7, "true", 5)
						if err != nil {
							t.Fatal(err)
						}
						ds = append(ds, d)
					}
					if withPost {
						for i, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure} {
							d, err := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackModel, domain.HookDigest(nil), []string{"post", "failure"}[i], 7, "true", 5)
							if err != nil {
								t.Fatal(err)
							}
							ds = append(ds, d)
						}
					}
					preInputs, postInputs := 0, 0
					runner := &postTestRunner{run: func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
						stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
						if id.Event.Post() {
							postInputs++
							if id.Event != domain.HookPostToolUse || !input.Input.Equal(final) || input.Response == nil {
								t.Fatal("post used original or wrong selected input")
							}
							b, err := input.Bytes()
							if err != nil || strings.Contains(string(b), "[PreToolUse input rewrite]") {
								t.Fatal("model annotation leaked into post stdin", err)
							}
							return domain.HookExecution{Result: stats}
						}
						preInputs++
						expected, next := before, middle
						if id.Ordinal == 1 {
							expected, next = middle, final
						}
						if !input.Input.Equal(expected) || preparations() != 0 || executions() != 0 {
							t.Fatal("hook input chain or execution order changed")
						}
						if id.Ordinal == 1 && mode == "reject" {
							stats.Status, stats.Code, stats.ExitCode = domain.HookBlock, domain.HookDenied, 2
							return domain.HookExecution{Result: stats}
						}
						replacement, err := domain.NewHookInputReplacement(next, input.RewriteContext().Profile())
						if err != nil {
							t.Fatal(err)
						}
						return domain.HookExecution{Result: stats, Replacement: replacement}
					}}
					snapshot, err := domain.NewHookSnapshot(ds, nil)
					if err != nil {
						t.Fatal(err)
					}
					if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
						t.Fatal(err)
					}
					approvals := 0
					baseEmit := postApprovalEmitter(t, r, original, protocol.HookTrustSession, tool.ApprovalAllowOnce)
					emit := func(e protocol.Event) {
						if e.Kind == protocol.EventToolApprovalRequested {
							approvals++
							if capability == tool.CapabilityExec {
								allowance, err := r.resultBodyAllowance(effective)
								if err != nil {
									t.Fatal(err)
								}
								candidate, err := r.execPreparer.(*rewriteExecPreparer).candidate.WithPreviewAllowance(allowance)
								if err != nil {
									t.Fatal(err)
								}
								fingerprint = candidate.Fingerprint()
								baseAllowance, err := r.resultAllowance(capability)
								if err != nil {
									t.Fatal(err)
								}
								oldCandidate, err := originalCandidates.execPreparer.(*rewriteExecPreparer).candidate.WithPreviewAllowance(baseAllowance)
								if err != nil {
									t.Fatal(err)
								}
								originalFingerprint = oldCandidate.Fingerprint()
							}
							p, err := protocol.DecodeToolApprovalRequested(e)
							if err != nil || p.Fingerprint != fingerprint {
								t.Fatal("approval did not bind effective candidate", err)
							}
							if mode != "revert" {
								requestID, _ := tool.ParseApprovalRequestID(p.RequestID)
								stale, err := tool.NewApprovalResponse(requestID, runtimeTurnID, original.InvocationID(), tool.CandidateFingerprint(originalFingerprint), tool.ApprovalAllowOnce, "")
								if err != nil {
									t.Fatal(err)
								}
								if err := r.ResolveToolApproval(stale); err == nil {
									t.Fatal("approval for original input authorized rewritten candidate")
								}
							}
							if capability == tool.CapabilityExec {
								command, _, _, _ := final.Exec()
								if p.Command != command {
									t.Fatal("approved original command")
								}
							} else {
								file, _, ok := final.Write()
								if !ok {
									file, _, _ = final.Patch()
								}
								if p.RelativePath != file {
									t.Fatal("approved original file")
								}
							}
						}
						baseEmit(e)
					}
					result, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, original)
					if err != nil || preInputs != 2 {
						t.Fatal("rewrite pipeline failed", err, preInputs)
					}
					state := r.toolCalls[original.InvocationID()]
					selection, err := state.selection()
					if err != nil {
						t.Fatal(err)
					}
					if !selection.Original().Equal(original) {
						t.Fatal("original ready overwritten")
					}
					if mode == "reject" {
						if result.Status() != tool.ResultDenied || preparations() != 0 || executions() != 0 || approvals != 0 || postInputs != 0 || selection.Kind() != tool.InputGateRejected || tool.ValidateResultInput(original, result) != nil {
							t.Fatal("rejected checking candidate reached execution")
						}
					} else {
						if result.Status() != tool.ResultSuccess || preparations() != 1 || executions() != 1 || approvals != 1 || state.admission.Sequence() == 0 || tool.ValidateResultInput(effective, result) != nil {
							t.Fatal("selected input not executed exactly once", result.Status(), preparations(), executions(), approvals)
						}
						if withPost && postInputs != 1 || !withPost && postInputs != 0 {
							t.Fatal("wrong post route")
						}
					}
					changed := mode != "revert" && mode != "reject"
					if strings.HasPrefix(result.Preview().Text(), "[PreToolUse input rewrite]\n") != changed {
						t.Fatal("wrong disclosure mode", result.Preview().Text())
					}
					if mode == "rewrite-then-noop" && state.pre.Finished[1].InputEffect.Kind() != domain.HookInputUnchanged {
						t.Fatal("equivalent replacement counted as change")
					}
					beforeBatches := len(j.snapshot())
					again, _, err := r.executeApproved(context.Background(), emit, runtimeTurnID, original)
					if err != nil || again.Preview() != result.Preview() || len(j.snapshot()) != beforeBatches || runner.calls != preInputs+postInputs {
						t.Fatal("completed rewrite reran or refroze", err)
					}
				})
			}
		}
	}
}

func TestRewriteReentryBindsActualIndexesAndRetires(t *testing.T) {
	r, inv, _, _ := hookRuntimeFixture(t, &fakeJournal{}, 0)
	state := r.toolCalls[inv.InvocationID()]
	state.origin.SampleIndex = 4
	if _, _, err := r.executeApproved(context.Background(), nil, runtimeTurnID, inv); err == nil {
		t.Fatal("changed real sample source accepted")
	}
	state.origin.SampleIndex = 0
	state.running = true
	if _, _, err := r.executeApproved(context.Background(), nil, runtimeTurnID, inv); err == nil {
		t.Fatal("in-flight call started another owner")
	}
	state.running = false
	r.resetToolTurn()
	if _, _, err := r.executeApproved(context.Background(), nil, runtimeTurnID, inv); err == nil || len(r.toolCalls) != 0 {
		t.Fatal("retired source authorized future call")
	}
}

func countRewriteFact(t *testing.T, j *fakeJournal, kind session.EventKind) int {
	t.Helper()
	count := 0
	for _, batch := range j.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == kind {
				count++
			}
		}
	}
	return count
}

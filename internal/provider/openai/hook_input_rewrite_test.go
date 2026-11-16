package openai

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"os"
	"strings"
	"testing"
)

func rewriteProviderOutput(t *testing.T, original tool.Invocation, mode string) tool.ModelOutput {
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

func TestPreInputRewriteNativeGoldenRestoreAndCache(t *testing.T) {
	for _, capability := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
		for _, mode := range []string{"selected", "unchanged", "revert", "rejected", "denied", "start-failed", "recovery-unadmitted", "recovery-admitted"} {
			t.Run(string(capability)+"/"+mode, func(t *testing.T) {
				result, call := postProviderFixture(t, capability)
				original := testNativeInvocation(t, call, result.InvocationID(), result.ProviderCallID())
				output := rewriteProviderOutput(t, original, mode)
				history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem("rewrite original")), {Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", ID: "pre-reason", EncryptedContent: "opaque-pre"}, call}}})
				entry, err := prepareToolOutputsEntry(history, []tool.ModelOutput{output})
				if err != nil {
					t.Fatal(err)
				}
				instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-rewrite-key"), Model: "fixture-model"})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				live := &Conversation{provider: instance, threadID: "01900000-0000-7000-8000-000000000001"}
				var envelopes []provider.NativeCommitEnvelope
				for _, e := range append(history, entry) {
					live.history.commit(e)
					native, err := encodeNativeCommit(e)
					if err != nil {
						t.Fatal(err)
					}
					envelopes = append(envelopes, native)
				}
				records := testRestoreRecords(t, envelopes, []tool.ModelOutput{output})
				restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
				if err != nil {
					t.Fatal(err)
				}
				restored := restoredValue.(*Conversation)
				sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testOpenAIToolCatalog(t))
				if err != nil {
					t.Fatal(err)
				}
				for _, policy := range []domain.CachePolicy{domain.DefaultCachePolicy(), mustStopCachePolicy(t)} {
					instance.config.CachePolicy = policy
					request, err := live.compileSamplingRequest(live.history.snapshot(), sampling)
					if err != nil {
						t.Fatal(err)
					}
					again, err := restored.compileSamplingRequest(restored.history.snapshot(), sampling)
					if err != nil || !bytes.Equal(request.Bytes(), again.Bytes()) {
						t.Fatal("restored request/cache bytes changed", err)
					}
					before, _ := live.HistoryFootprint()
					after, _ := restored.HistoryFootprint()
					if before != after {
						t.Fatal("native item order/fingerprint/revision changed")
					}
					if !bytes.Contains(request.Bytes(), []byte("opaque-pre")) || bytes.Contains(request.Bytes(), []byte("fixture-rewrite-key")) {
						t.Fatal("opaque evidence lost or credentials persisted")
					}
					full, err := websocketBody(request, "", 0)
					if err != nil {
						t.Fatal(err)
					}
					var fullFrame websocketCreate
					if err := codec.Unmarshal(full.Bytes(), &fullFrame); err != nil {
						t.Fatal(err)
					}
					incremental, err := websocketBody(request, "pre-response", len(fullFrame.Input)-1)
					if err != nil {
						t.Fatal(err)
					}
					var whole, suffix websocketCreate
					if codec.Unmarshal(full.Bytes(), &whole) != nil || codec.Unmarshal(incremental.Bytes(), &suffix) != nil || suffix.PreviousResponseID != "pre-response" || len(suffix.Input) != 1 || whole.PromptCacheKey != suffix.PromptCacheKey {
						t.Fatal("rewrite websocket frame/key mismatch")
					}
					reconstructed := append(append([]NativeItem(nil), whole.Input[:len(whole.Input)-1]...), suffix.Input...)
					a, _ := codec.MarshalStable(whole.Input)
					b, _ := codec.MarshalStable(reconstructed)
					if !bytes.Equal(a, b) {
						t.Fatal("incremental/full logical native order differs")
					}
					if mode == "selected" {
						path := "testdata/pre_rewrite_" + string(capability) + "_" + string(policy.Key()) + ".golden.json"
						if os.Getenv("EASYCODE_UPDATE_REWRITE_GOLDENS") == "1" {
							if err := os.WriteFile(path, append(request.Bytes(), '\n'), 0600); err != nil {
								t.Fatal(err)
							}
						}
						want, err := os.ReadFile(path)
						if err != nil || !bytes.Equal(bytes.TrimSpace(want), request.Bytes()) {
							t.Fatal("rewrite request golden changed", err)
						}
					}
				}
				frozen, err := codec.MarshalStable(history)
				if err != nil || !bytes.Contains(frozen, []byte("main.go")) && capability == domain.HookPatch {
					t.Fatal("original native call replaced", err)
				}
				if mode == "selected" || mode == "start-failed" {
					if !strings.Contains(output.Text(), "Admitted tool attempt") {
						t.Fatal("admission disclosure missing")
					}
				}
				if mode == "unchanged" || mode == "revert" || mode == "rejected" {
					if strings.HasPrefix(output.Text(), "[PreToolUse input rewrite]\n") {
						t.Fatal("unchanged/unselected input disclosed as selected")
					}
				}
				// 合法 checksum 的原生输出伪造必须整体失败，不安装部分历史。
				forged := entry.clone()
				forged.ToolOutputs[0].Output = `{"status":"success","code":"ok","content":"forged"}`
				changed, err := encodeNativeCommit(forged)
				if err != nil {
					t.Fatal(err)
				}
				records[len(records)-1], err = provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), changed, []tool.ModelOutput{output})
				if err != nil {
					t.Fatal(err)
				}
				if value, err := instance.RestoreConversation("01900000-0000-7000-8000-000000000001", records); err == nil || value != nil {
					t.Fatal("forged effective input/output accepted")
				}
				// ready 必须仍为 O；把 E 塞进原始 sample 关联也必须拒绝。
				records = testRestoreRecords(t, envelopes, []tool.ModelOutput{output})
				if output.Evidence().Selection().Changed() {
					effective, _ := output.Evidence().Selection().ResultInvocation()
					records[1], err = records[1].WithReadyFacts([]tool.Invocation{effective}, nil)
					if err != nil {
						t.Fatal(err)
					}
					if value, err := instance.RestoreConversation("01900000-0000-7000-8000-000000000001", records); err == nil || value != nil {
						t.Fatal("effective input impersonated original native call")
					}
				}
				if mode == "selected" {
					assertCompactionAfterToolSteering(t, instance, live, sampling, envelopes, []tool.ModelOutput{output})
				}
			})
		}
	}
}

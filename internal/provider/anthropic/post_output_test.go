package anthropic

import (
	"bytes"
	"context"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"os"
	"strings"
	"testing"
)

func postProviderFixture(t *testing.T, capability domain.HookCapability) (tool.InvocationResult, NativeItem) {
	t.Helper()
	id := tool.InvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	var result tool.InvocationResult
	var call NativeItem
	var err error
	switch capability {
	case domain.HookPatch:
		replacement, _ := tool.NewPatchReplacement("old", "new", false)
		input, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
		ready, _ := tool.NewPatchReadyCall("call-post", tool.WireStructured, input)
		inv, _ := tool.NewPatchInvocation(id, ready)
		metadata, _ := tool.NewPatchResultMetadata("main.go", 1, 1, 1)
		preview, _ := tool.NewModelPreview("Updated main.go (1 replacements, +1 -1 lines)")
		result, err = tool.NewPatchInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
		call = NativeItem{Type: blockTypeToolUse, ID: "call-post", Name: "Edit", Input: []byte(`{"file_path":"main.go","old_string":"old","new_string":"new"}`)}
	case domain.HookWrite:
		input, _ := tool.NewWriteInput("written", "new")
		ready, _ := tool.NewWriteReadyCall("call-post", input)
		inv, _ := tool.NewWriteInvocation(id, ready)
		metadata, _ := tool.NewWriteResultMetadata("written", tool.WriteCreate, 3, 1, 0)
		preview, _ := tool.NewModelPreview("Created written (3 bytes, 1 lines, 0 directories created)")
		result, err = tool.NewWriteInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
		call = NativeItem{Type: blockTypeToolUse, ID: "call-post", Name: "Write", Input: []byte(`{"file_path":"written","content":"new"}`)}
	case domain.HookExec:
		input, _ := tool.NewExecInput("exit 7", "", 1000)
		ready, _ := tool.NewExecReadyCall("call-post", input)
		inv, _ := tool.NewExecInvocation(id, ready)
		metadata, _ := tool.NewExecResultMetadata("", 7, true, 1, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
		preview, _ := tool.NewModelPreview("Exec completed with exit 7")
		result, err = tool.NewExecInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
		call = NativeItem{Type: blockTypeToolUse, ID: "call-post", Name: "Bash", Input: []byte(`{"command":"exit 7","timeout_ms":1000}`)}
	}
	if err != nil || result.Validate() != nil {
		t.Fatal("invalid provider post fixture", err)
	}
	result, err = tool.FinalizeResult(result, 240<<10, tool.BudgetPreserved)
	if err != nil {
		t.Fatal(err)
	}
	return result, call
}
func postProviderOutputAtCut(t *testing.T, result tool.InvocationResult, cut string) tool.ModelOutput {
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
	output, err := tool.NewPostModelOutput(result, selection, []domain.FrozenHookFeedback{feedback})
	if err != nil {
		t.Fatal(err)
	}
	return output
}
func TestPostOutputGoldenRestoreAndForgedBody(t *testing.T) {
	testPostOutputGoldenRestore(t, false, "completed")
}
func TestFailureOutputGoldenRestoreAndForgedBody(t *testing.T) {
	for _, cut := range []string{"completed", "between", "start-failed", "cancelled"} {
		t.Run(cut, func(t *testing.T) { testPostOutputGoldenRestore(t, true, cut) })
	}
}
func testPostOutputGoldenRestore(t *testing.T, failed bool, cut string) {
	for _, capability := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
		if cut == "start-failed" && capability != domain.HookExec {
			continue
		}
		t.Run(string(capability), func(t *testing.T) {
			result, call := postProviderFixture(t, capability)
			if failed {
				result = postFailureProviderResult(t, result)
				if cut == "start-failed" {
					result = postFailureProviderResultWithCode(t, result, "process_start_failed")
				}
			}
			output := postProviderOutputAtCut(t, result, cut)
			history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage("inspect post")), {Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "private post", Signature: "opaque-post"}, call}}, Metadata: messageMetadata{ID: "post-message", Model: "fixture-model"}}})
			entry, err := prepareToolOutputsEntry(history, []tool.ModelOutput{output})
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := encodeNativeCommit(entry)
			if err != nil {
				t.Fatal(err)
			}
			path := "testdata/post_" + string(capability) + "_output.golden.json"
			if failed {
				path = strings.Replace(path, "testdata/post_", "testdata/failure_", 1)
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if cut == "completed" && !bytes.Equal(bytes.TrimSpace(want), envelope.Payload()) {
				t.Fatalf("post output golden changed: %s", envelope.Payload())
			}
			instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			live := &Conversation{provider: instance}
			var envelopes []provider.NativeCommitEnvelope
			for _, value := range append(history, entry) {
				live.history.commit(value)
				e, err := encodeNativeCommit(value)
				if err != nil {
					t.Fatal(err)
				}
				envelopes = append(envelopes, e)
			}
			records := testRestoreRecords(t, envelopes, []tool.ModelOutput{output})
			restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
			if err != nil {
				t.Fatal(err)
			}
			restored := restoredValue.(*Conversation)
			first, err := compileMessagesRequest("fixture-model", int(instance.config.Inference.MaxOutputTokens()), live.historySnapshot(), nil, testAnthropicToolView(t))
			if err != nil {
				t.Fatal(err)
			}
			second, err := compileMessagesRequest("fixture-model", int(instance.config.Inference.MaxOutputTokens()), restored.historySnapshot(), nil, testAnthropicToolView(t))
			if err != nil || !bytes.Equal(first.Bytes(), second.Bytes()) {
				t.Fatal("post restore changed canonical request", err)
			}
			before, _ := live.HistoryFootprint()
			after, _ := restored.HistoryFootprint()
			if before != after || !bytes.Contains(second.Bytes(), []byte("opaque-post")) {
				t.Fatal("post restore changed fingerprint/order/opaque evidence")
			}
			plain, err := tool.NewModelOutput(result)
			if err != nil || plain.Text() != result.Preview().Text() || output.Result().Status() != result.Status() || !(strings.Contains(output.Text(), "Read-only check:") || cut == "cancelled" && strings.Contains(output.Text(), "cancelled")) {
				t.Fatal("composition rewrote result", err)
			}
			forged := entry.clone()
			forged.ToolOutputs.Content[0].Content = "forged body"
			changed, err := encodeNativeCommit(forged)
			if err != nil {
				t.Fatal(err)
			}
			records[len(records)-1], err = provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), changed, []tool.ModelOutput{output})
			if err != nil {
				t.Fatal(err)
			}
			reader := NewHistoryReader()
			if _, err := reader.ProjectStoredHistory(context.Background(), provider.HistorySource{Family: instance.Family(), Wire: messagesWire, Model: "fixture-model"}, records); err == nil {
				t.Fatal("forged native output accepted")
			}
			if _, err := provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), envelope, nil); err == nil {
				t.Fatal("missing frozen evidence accepted")
			}
			if _, err := provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), envelope, []tool.ModelOutput{output, output}); err == nil {
				t.Fatal("duplicate frozen evidence accepted")
			}
			// 反馈必须进入真实摘要请求；连续压缩与恢复仍保存原工具位置、opaque 和缓存证据。
			sampling, err := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			compact, err := provider.NewCompactionInput(sampling, "preserve checks", 4096)
			if err != nil {
				t.Fatal(err)
			}
			request, err := live.compileCompactionRequest(compact)
			if err != nil || !(bytes.Contains(request.Bytes(), []byte("Read-only check:")) || cut == "cancelled" && bytes.Contains(request.Bytes(), []byte("cancelled"))) {
				t.Fatal("post feedback missing from compaction source", err)
			}
			assertCompactionAfterToolSteering(t, instance, live, sampling, envelopes, []tool.ModelOutput{output})
		})
	}
}

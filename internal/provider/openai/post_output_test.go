package openai

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
		ready, _ := tool.NewPatchReadyCall("call-post", tool.WireCustomFreeform, input)
		inv, _ := tool.NewPatchInvocation(id, ready)
		metadata, _ := tool.NewPatchResultMetadata("main.go", 1, 1, 1)
		preview, _ := tool.NewModelPreview("Updated main.go (1 replacements, +1 -1 lines)")
		result, err = tool.NewPatchInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
		call = NativeItem{Type: "custom_tool_call", ID: "item-post", CallID: "call-post", Name: "apply_patch", Input: "*** Begin Patch\n*** Update File: main.go\n@@\n-old\n+new\n*** End Patch\n"}
	case domain.HookWrite:
		input, _ := tool.NewWriteInput("written", "new")
		ready, _ := tool.NewWriteReadyCall("call-post", input)
		inv, _ := tool.NewWriteInvocation(id, ready)
		metadata, _ := tool.NewWriteResultMetadata("written", tool.WriteCreate, 3, 1, 0)
		preview, _ := tool.NewModelPreview("Created written (3 bytes, 1 lines, 0 directories created)")
		result, err = tool.NewWriteInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
		call = NativeItem{Type: "function_call", ID: "item-post", CallID: "call-post", Name: "Write", Arguments: `{"file_path":"written","content":"new"}`}
	case domain.HookExec:
		input, _ := tool.NewExecInput("exit 7", "", 1000)
		ready, _ := tool.NewExecReadyCall("call-post", input)
		inv, _ := tool.NewExecInvocation(id, ready)
		metadata, _ := tool.NewExecResultMetadata("", 7, true, 1, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
		preview, _ := tool.NewModelPreview("Exec completed with exit 7")
		result, err = tool.NewExecInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
		call = NativeItem{Type: "function_call", ID: "item-post", CallID: "call-post", Name: "exec_command", Arguments: `{"command":"exit 7","timeout_ms":1000}`}
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
func postProviderOutput(t *testing.T, result tool.InvocationResult) tool.ModelOutput {
	t.Helper()
	d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackModel, strings.Repeat("a", 64), "check", 7, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := domain.NewHookPlan(string(result.InvocationID()), domain.HookCapability(result.Capability()), []domain.HookReference{d.Reference})
	plan, _ := domain.NewPostHookPlan(p, 256<<10, 16<<10, domain.HookDeliveryAvailable)
	id, _ := domain.NewPostHookRunIdentity(plan, 0)
	stats := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution", StdoutBytes: 100}
	candidate, _ := domain.NewHookFeedbackCandidate("Read-only check: keep TODO visible.")
	frozen, err := tool.FreezePostFeedback(plan, nil, stats, candidate)
	if err != nil {
		t.Fatal(err)
	}
	output, err := tool.NewModelOutput(result, []domain.FrozenHookFeedback{frozen.Feedback})
	if err != nil {
		t.Fatal(err)
	}
	return output
}
func TestPostOutputGoldenRestoreAndForgedBody(t *testing.T) {
	for _, capability := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
		t.Run(string(capability), func(t *testing.T) {
			result, call := postProviderFixture(t, capability)
			output := postProviderOutput(t, result)
			history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem("inspect post")), {Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", ID: "reason-post", EncryptedContent: "opaque-post"}, call}}})
			entry, err := prepareToolOutputsEntry(history, []tool.ModelOutput{output})
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := encodeNativeCommit(entry)
			if err != nil {
				t.Fatal(err)
			}
			path := "testdata/post_" + string(capability) + "_output.golden.json"
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(bytes.TrimSpace(want), envelope.Payload()) {
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
			first, err := compileResponsesRequest("fixture-model", live.historySnapshot(), nil, testOpenAIToolView(t))
			if err != nil {
				t.Fatal(err)
			}
			second, err := compileResponsesRequest("fixture-model", restored.historySnapshot(), nil, testOpenAIToolView(t))
			if err != nil || !bytes.Equal(first.Bytes(), second.Bytes()) {
				t.Fatal("post restore changed canonical request", err)
			}
			before, _ := live.HistoryFootprint()
			after, _ := restored.HistoryFootprint()
			if before != after || !bytes.Contains(second.Bytes(), []byte("opaque-post")) {
				t.Fatal("post restore changed fingerprint/order/opaque evidence")
			}
			plain, err := tool.NewModelOutput(result, nil)
			if err != nil || plain.Text() != result.Preview().Text() || output.Result().Status() != tool.ResultSuccess || !strings.Contains(output.Text(), "Read-only check:") {
				t.Fatal("composition rewrote result", err)
			}
			forged := entry.clone()
			forged.ToolOutputs[0].Output = `{"status":"success","code":"ok","content":"forged body"}`
			changed, err := encodeNativeCommit(forged)
			if err != nil {
				t.Fatal(err)
			}
			records[len(records)-1], err = provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), changed, []tool.ModelOutput{output})
			if err != nil {
				t.Fatal(err)
			}
			reader := NewHistoryReader()
			if _, err := reader.ProjectStoredHistory(context.Background(), provider.HistorySource{Family: instance.Family(), Wire: responsesWire, Model: "fixture-model"}, records); err == nil {
				t.Fatal("forged native output accepted")
			}
			if _, err := provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), envelope, nil); err == nil {
				t.Fatal("missing frozen evidence accepted")
			}
			if _, err := provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), envelope, []tool.ModelOutput{output, output}); err == nil {
				t.Fatal("duplicate frozen evidence accepted")
			}
			// 反馈必须进入真实摘要请求；连续压缩与恢复仍保存原工具位置、opaque 和缓存证据。
			sampling, err := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			compact, err := provider.NewCompactionInput(sampling, "preserve checks", 4096)
			if err != nil {
				t.Fatal(err)
			}
			request, err := live.compileCompactionRequest(compact)
			if err != nil || !bytes.Contains(request.Bytes(), []byte("Read-only check:")) {
				t.Fatal("post feedback missing from compaction source", err)
			}
			assertCompactionAfterToolSteering(t, instance, live, sampling, envelopes, []tool.ModelOutput{output})
		})
	}
}

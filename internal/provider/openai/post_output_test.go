package openai

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func postProviderFixture(t *testing.T, capability domain.HookCapability) (tool.InvocationResult, NativeItem) {
	t.Helper()
	var call NativeItem
	switch capability {
	case domain.HookPatch:
		call = NativeItem{Type: "custom_tool_call", ID: "item-post", CallID: "call-post", Name: "apply_patch", Input: "*** Begin Patch\n*** Update File: main.go\n@@\n-old\n+new\n*** End Patch\n"}
	case domain.HookWrite:
		call = NativeItem{Type: "function_call", ID: "item-post", CallID: "call-post", Name: "Write", Arguments: `{"file_path":"written","content":"new"}`}
	case domain.HookExec:
		call = NativeItem{Type: "function_call", ID: "item-post", CallID: "call-post", Name: "exec_command", Arguments: `{"command":"exit 7","timeout_ms":1000}`}
	}
	return testsupport.PostResult(t, capability, tool.WireCustomFreeform), call
}

func postProviderOutputAtCut(t *testing.T, result tool.InvocationResult, cut string) tool.ModelOutput {
	t.Helper()
	_, native := postProviderFixture(t, domain.HookCapability(result.Capability()))
	original := testNativeInvocation(t, native, result.InvocationID(), result.ProviderCallID())
	return testsupport.PostOutputAtCut(t, original, result, cut)
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
				result = testsupport.FailureResult(t, result)
				if cut == "start-failed" {
					result = testsupport.FailureResultWithCode(t, result, "process_start_failed")
				}
			}
			output := postProviderOutputAtCut(t, result, cut)
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
				live.history.Append(value)
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
			sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			first := testsupport.RestoredRequest(t, live, restored, sampling)
			if !bytes.Contains(first.Bytes(), []byte("opaque-post")) {
				t.Fatal("post restore lost opaque evidence")
			}
			plain, err := tool.NewModelOutput(result, output.Evidence())
			if err != nil || plain.Text() != result.Preview().Text() || output.Result().Status() != result.Status() || !(strings.Contains(output.Text(), "Read-only check:") || cut == "cancelled" && strings.Contains(output.Text(), "cancelled")) {
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
			if _, err := reader.ProjectStoredHistory(context.Background(), provider.HistorySource{ThreadID: "01900000-0000-7000-8000-000000000001", Family: instance.Family(), Wire: responsesWire, Model: "fixture-model"}, records, nil); err == nil {
				t.Fatal("forged native output accepted")
			}
			if _, err := provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), envelope, nil); err == nil {
				t.Fatal("missing frozen evidence accepted")
			}
			if _, err := provider.NewToolOutputRestoreRecord(records[len(records)-1].TurnID(), envelope, []tool.ModelOutput{output, output}); err == nil {
				t.Fatal("duplicate frozen evidence accepted")
			}
			// 反馈必须进入真实摘要请求；连续压缩与恢复仍保存原工具位置、opaque 和缓存证据。
			compact, err := provider.NewCompactionInput(sampling, "preserve checks", 4096)
			if err != nil {
				t.Fatal(err)
			}
			request, err := live.compileCompactionRequest(live.history.Snapshot(), compact)
			if err != nil || !(bytes.Contains(request.Bytes(), []byte("Read-only check:")) || cut == "cancelled" && bytes.Contains(request.Bytes(), []byte("cancelled"))) {
				t.Fatal("post feedback missing from compaction source", err)
			}
			assertCompactionAfterToolSteering(t, instance, live, sampling, envelopes, []tool.ModelOutput{output})
		})
	}
}

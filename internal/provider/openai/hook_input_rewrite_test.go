package openai

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestPreInputRewriteNativeGoldenRestoreAndCache(t *testing.T) {
	for _, capability := range []domain.HookCapability{domain.HookPatch, domain.HookWrite, domain.HookExec} {
		for _, mode := range []string{"selected", "unchanged", "revert", "rejected", "denied", "start-failed", "recovery-unadmitted", "recovery-admitted"} {
			// 三种 wire 均保留 selected golden；其余证据状态复用 Exec 代表接线。
			if capability != domain.HookExec && mode != "selected" {
				continue
			}
			t.Run(string(capability)+"/"+mode, func(t *testing.T) {
				result, call := postProviderFixture(t, capability)
				original := testNativeInvocation(t, call, result.InvocationID(), result.ProviderCallID())
				output := testsupport.RewriteOutput(t, original, mode)
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
					live.history.Append(e)
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
				sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
				if err != nil {
					t.Fatal(err)
				}
				policies := []domain.CachePolicy{domain.DefaultCachePolicy()}
				if mode == "selected" {
					policies = []domain.CachePolicy{domain.DefaultCachePolicy(), mustStopCachePolicy(t)}
				}
				for _, policy := range policies {
					instance.config.CachePolicy = policy
					request := testsupport.RestoredRequest(t, live, restored, sampling)
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
				if value, _, err := instance.RestoreConversation(context.Background(), "01900000-0000-7000-8000-000000000001", records); err == nil || value != nil {
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
					if value, _, err := instance.RestoreConversation(context.Background(), "01900000-0000-7000-8000-000000000001", records); err == nil || value != nil {
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

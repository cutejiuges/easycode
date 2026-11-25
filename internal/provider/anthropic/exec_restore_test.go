package anthropic

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestExecNativeResultsRestoreExactly(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "fixture-model", Inference: testInference(t, 1024)})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range testsupport.ExecRestoreResults(t) {
		t.Run(test.Name, func(t *testing.T) {
			result := test.Result
			preview := result.Preview()
			history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage("test")), {Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"}, {Type: blockTypeToolUse, ID: "call-exec", Name: "Bash", Input: json.RawMessage(`{"command":"printf '你好'","workdir":"src","timeout_ms":1000}`)}}}, Metadata: messageMetadata{ID: "msg-exec", Model: "fixture-model"}}})
			evidence := testModelOutputs(t, history, result)
			output, err := prepareToolOutputsEntry(history, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if len(output.ToolOutputs.Content) != 1 {
				t.Fatal("Exec output count changed")
			}
			block := output.ToolOutputs.Content[0]
			if block.Type != blockTypeToolResult || block.ToolUseID != "call-exec" || block.Content != preview.Text() || block.IsError != (result.Status() != tool.ResultSuccess) {
				t.Fatal("Exec frozen result or pairing changed")
			}
			history = append(history, output)
			uninterrupted, restored := restoreToolHistory(t, instance, history, evidence)
			sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			first := testsupport.RestoredRequest(t, uninterrupted, restored, sampling)
			if test.Name == "zero" || test.Name == "hook-denied" {
				golden := "testdata/exec_continuation.golden.json"
				if test.Name == "hook-denied" {
					golden = "testdata/hook_denied_continuation.golden.json"
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(first.Bytes(), bytes.TrimSpace(want)) {
					t.Fatalf("Exec continuation golden mismatch:\n%s", first.Bytes())
				}
			}
			if test.Name == "zero" {
				assertPlanExecRestore(t, uninterrupted, restored)
			}

		})
	}
}

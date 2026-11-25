package openai

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestExecNativeResultsRestoreExactly(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	for _, test := range testsupport.ExecRestoreResults(t) {
		t.Run(test.Name, func(t *testing.T) {
			result := test.Result
			preview := result.Preview()
			history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem("test")), {Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", ID: "reason-exec", EncryptedContent: "opaque-reason"}, {Type: "function_call", ID: "fc-exec", CallID: "call-exec", Name: "exec_command", Arguments: `{"command":"printf '你好'","workdir":"src","timeout_ms":1000}`}}}})
			evidence := testModelOutputs(t, history, result)
			output, err := prepareToolOutputsEntry(history, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if len(output.ToolOutputs) != 1 || output.ToolOutputs[0].Type != "function_call_output" || output.ToolOutputs[0].CallID != "call-exec" {
				t.Fatal("Exec pairing lost")
			}
			var frozen struct {
				Status  string `json:"status"`
				Code    string `json:"code"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(output.ToolOutputs[0].Output), &frozen); err != nil {
				t.Fatal(err)
			}
			if frozen.Content != preview.Text() || frozen.Status != string(result.Status()) || frozen.Code != result.Code() {
				t.Fatal("Exec frozen result changed")
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

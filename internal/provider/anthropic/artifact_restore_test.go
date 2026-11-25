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

func TestReadArtifactNativeResultsRestoreExactly(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "fixture-model", Inference: testInference(t, 1024)})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	input, _ := tool.NewReadArtifactInput("0123456789abcdef0123456789abcdef", 0, 3)
	call, _ := tool.NewReadArtifactReadyCall("call-exec", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewReadArtifactInvocation(id, call)
	for _, test := range []struct {
		name     string
		status   tool.ResultStatus
		code     string
		complete bool
	}{
		{"complete", tool.ResultSuccess, "ok", true}, {"partial", tool.ResultSuccess, "ok", false}, {"unavailable", tool.ResultError, "unavailable", false}, {"cancelled", tool.ResultCancelled, "cancelled", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			preview, _ := tool.NewModelPreview("ReadArtifact failed: " + test.code)
			var page tool.ArtifactPage
			if test.status == tool.ResultSuccess {
				page, err = tool.NewArtifactPage(input.ID(), 0, 3, 3, test.complete, "中")
				if err != nil {
					t.Fatal(err)
				}
				preview, err = page.Preview()
				if err != nil {
					t.Fatal(err)
				}
			}
			result, err := tool.NewReadArtifactInvocationResult(invocation, test.status, test.code, preview, page)
			if err != nil {
				t.Fatal(err)
			}
			history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage("test")), {Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"}, {Type: blockTypeToolUse, ID: "call-exec", Name: "ReadArtifact", Input: json.RawMessage(`{"artifact_id":"0123456789abcdef0123456789abcdef","offset":0,"limit":3}`)}}}, Metadata: messageMetadata{ID: "msg-exec", Model: "fixture-model"}}})
			evidence := testModelOutputs(t, history, result)
			output, err := prepareToolOutputsEntry(history, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if len(output.ToolOutputs.Content) != 1 {
				t.Fatal("ReadArtifact output count changed")
			}
			block := output.ToolOutputs.Content[0]
			if block.Type != blockTypeToolResult || block.ToolUseID != "call-exec" || block.Content != preview.Text() || block.IsError != (test.status != tool.ResultSuccess) {
				t.Fatal("ReadArtifact frozen result or pairing changed")
			}
			history = append(history, output)
			uninterrupted, restored := restoreToolHistory(t, instance, history, evidence)
			sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			first := testsupport.RestoredRequest(t, uninterrupted, restored, sampling)
			if test.name == "complete" {
				want, err := os.ReadFile("testdata/artifact_continuation.golden.json")
				if err != nil || !bytes.Equal(first.Bytes(), bytes.TrimSpace(want)) {
					t.Fatalf("ReadArtifact continuation golden mismatch:\n%s", first.Bytes())
				}
			}

		})
	}
}

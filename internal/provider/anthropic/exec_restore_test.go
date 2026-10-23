package anthropic

import (
	"bytes"
	contextplan "easycode/internal/context"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"encoding/json"
	"os"
	"testing"
)

func TestExecNativeResultsRestoreExactly(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "fixture-model", MaxOutputTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	input, _ := tool.NewExecInput("printf '你好'", "src", 1000)
	call, _ := tool.NewExecReadyCall("call-exec", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, call)
	for _, test := range []struct {
		name                      string
		status                    tool.ResultStatus
		code                      string
		exit                      int
		known, timeout, cancelled bool
	}{
		{"zero", tool.ResultSuccess, "ok", 0, true, false, false},
		{"nonzero", tool.ResultSuccess, "ok", 2, true, false, false},
		{"denied", tool.ResultDenied, "approval_required", 0, false, false, false},
		{"cancelled", tool.ResultCancelled, "cancelled", 143, true, false, true},
		{"timeout", tool.ResultError, "timed_out", 137, true, true, false},
		{"uncertain", tool.ResultOutcomeUncertain, "process_cleanup_uncertain", 0, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			preview, _ := tool.NewModelPreview("frozen " + test.name + "\n你好\n")
			metadata, err := tool.NewExecResultMetadata("src", test.exit, test.known, 0, tool.ExecStreamStats{}, tool.ExecStreamStats{}, test.timeout, test.cancelled, false)
			if err != nil {
				t.Fatal(err)
			}
			result, err := tool.NewExecInvocationResult(invocation, test.status, test.code, preview, metadata)
			if err != nil {
				t.Fatal(err)
			}
			history := []nativeHistoryEntry{{Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("test")), Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"}, {Type: blockTypeToolUse, ID: "call-exec", Name: "Bash", Input: json.RawMessage(`{"command":"printf '你好'","workdir":"src","timeout_ms":1000}`)}}}, Metadata: messageMetadata{ID: "msg-exec", Model: "fixture-model"}}}
			output, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result})
			if err != nil {
				t.Fatal(err)
			}
			if len(output.ToolOutputs.Content) != 1 {
				t.Fatal("Exec output count changed")
			}
			block := output.ToolOutputs.Content[0]
			if block.Type != blockTypeToolResult || block.ToolUseID != "call-exec" || block.Content != preview.Text() || block.IsError != (test.status != tool.ResultSuccess) {
				t.Fatal("Exec frozen result or pairing changed")
			}
			history = append(history, output)
			uninterrupted := &Conversation{provider: instance}
			var commits []provider.NativeCommitEnvelope
			for _, entry := range history {
				uninterrupted.history.commit(entry)
				commit, err := encodeNativeCommit(entry)
				if err != nil {
					t.Fatal(err)
				}
				commits = append(commits, commit)
			}
			restoredValue, err := instance.RestoreConversation(commits)
			if err != nil {
				t.Fatal(err)
			}
			restored := restoredValue.(*Conversation)
			first, err := compileMessagesRequest(instance.config.Model, instance.config.MaxOutputTokens, uninterrupted.historySnapshot(), nil, nil, testAnthropicToolView(t))
			if err != nil {
				t.Fatal(err)
			}
			second, err := compileMessagesRequest(instance.config.Model, instance.config.MaxOutputTokens, restored.historySnapshot(), nil, nil, testAnthropicToolView(t))
			if err != nil {
				t.Fatal(err)
			}
			firstSegment, err := contextplan.NewSegment("request", contextplan.StabilityTurnStable, "current", first)
			if err != nil {
				t.Fatal(err)
			}
			secondSegment, err := contextplan.NewSegment("request", contextplan.StabilityTurnStable, "current", second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first.Bytes(), second.Bytes()) || firstSegment.Fingerprint() != secondSegment.Fingerprint() {
				t.Fatal("Exec restore changed canonical request or fingerprint")
			}
			if test.name == "zero" {
				want, err := os.ReadFile("testdata/exec_continuation.golden.json")
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(first.Bytes(), bytes.TrimSpace(want)) {
					t.Fatalf("Exec continuation golden mismatch:\n%s", first.Bytes())
				}
			}
			a, err := uninterrupted.HistoryFootprint()
			if err != nil {
				t.Fatal(err)
			}
			b, err := restored.HistoryFootprint()
			if err != nil || a != b {
				t.Fatal("Exec restore changed history footprint")
			}
		})
	}
}

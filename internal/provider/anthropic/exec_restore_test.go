package anthropic

import (
	"bytes"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestExecNativeResultsRestoreExactly(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "fixture-model", Inference: testInference(t, 1024)})
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
		{"hook-denied", tool.ResultDenied, "hook_denied", 0, false, false, false},
		{"hook-uncertain", tool.ResultCancelled, "hook_outcome_uncertain", 0, false, false, true},
		{"cancelled", tool.ResultCancelled, "cancelled", 143, true, false, true},
		{"timeout", tool.ResultError, "timed_out", 137, true, true, false},
		{"uncertain", tool.ResultOutcomeUncertain, "process_cleanup_uncertain", 0, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := "frozen " + test.name + "\n你好\n"
			if strings.HasPrefix(test.name, "hook-") {
				d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "check", 7, "true", 5)
				if err != nil {
					t.Fatal(err)
				}
				text = domain.HookSummary(d.Reference, domain.HookExec, domain.HookCode(test.code))
			}
			preview, _ := tool.NewModelPreview(text)
			metadata, err := tool.NewExecResultMetadata("src", test.exit, test.known, 0, tool.ExecStreamStats{}, tool.ExecStreamStats{}, test.timeout, test.cancelled, false)
			if err != nil {
				t.Fatal(err)
			}
			result, err := tool.NewExecInvocationResult(invocation, test.status, test.code, preview, metadata)
			if err != nil {
				t.Fatal(err)
			}
			history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage("test")), {Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-signature"}, {Type: blockTypeToolUse, ID: "call-exec", Name: "Bash", Input: json.RawMessage(`{"command":"printf '你好'","workdir":"src","timeout_ms":1000}`)}}}, Metadata: messageMetadata{ID: "msg-exec", Model: "fixture-model"}}})
			output, err := prepareToolOutputsEntry(history, testModelOutputs(t, result))
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
			restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, commits, testModelOutputs(t, result)))
			if err != nil {
				t.Fatal(err)
			}
			restored := restoredValue.(*Conversation)
			first, err := compileMessagesRequest(instance.config.Model, int(instance.config.Inference.MaxOutputTokens()), uninterrupted.historySnapshot(), nil, testAnthropicToolView(t))
			if err != nil {
				t.Fatal(err)
			}
			second, err := compileMessagesRequest(instance.config.Model, int(instance.config.Inference.MaxOutputTokens()), restored.historySnapshot(), nil, testAnthropicToolView(t))
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
			if test.name == "zero" || test.name == "hook-denied" {
				golden := "testdata/exec_continuation.golden.json"
				if test.name == "hook-denied" {
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

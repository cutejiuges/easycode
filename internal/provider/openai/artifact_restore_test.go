package openai

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

func TestReadArtifactNativeResultsRestoreExactly(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid/v1", APIKey: secret.New("fixture-key"), Model: "fixture-model"})
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
			history := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem("test")), {Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", ID: "reason-exec", EncryptedContent: "opaque-reason"}, {Type: "function_call", ID: "fc-exec", CallID: "call-exec", Name: "ReadArtifact", Arguments: `{"artifact_id":"0123456789abcdef0123456789abcdef","offset":0,"limit":3}`}}}})
			output, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result})
			if err != nil {
				t.Fatal(err)
			}
			if len(output.ToolOutputs) != 1 || output.ToolOutputs[0].Type != "function_call_output" || output.ToolOutputs[0].CallID != "call-exec" {
				t.Fatal("ReadArtifact pairing lost")
			}
			var frozen struct {
				Status  string `json:"status"`
				Code    string `json:"code"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(output.ToolOutputs[0].Output), &frozen); err != nil {
				t.Fatal(err)
			}
			if frozen.Content != preview.Text() || frozen.Status != string(test.status) || frozen.Code != test.code {
				t.Fatal("ReadArtifact frozen result changed")
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
			restoredValue, err := instance.RestoreConversation(testRestoreRecords(t, commits))
			if err != nil {
				t.Fatal(err)
			}
			restored := restoredValue.(*Conversation)
			first, err := compileResponsesRequest(instance.config.Model, uninterrupted.historySnapshot(), nil, testOpenAIToolView(t))
			if err != nil {
				t.Fatal(err)
			}
			second, err := compileResponsesRequest(instance.config.Model, restored.historySnapshot(), nil, testOpenAIToolView(t))
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
				t.Fatal("ReadArtifact restore changed canonical request or fingerprint")
			}
			if test.name == "complete" {
				want, err := os.ReadFile("testdata/artifact_continuation.golden.json")
				if err != nil || !bytes.Equal(first.Bytes(), bytes.TrimSpace(want)) {
					t.Fatalf("ReadArtifact continuation golden mismatch:\n%s", first.Bytes())
				}
			}
			a, err := uninterrupted.HistoryFootprint()
			if err != nil {
				t.Fatal(err)
			}
			b, err := restored.HistoryFootprint()
			if err != nil || a != b {
				t.Fatal("ReadArtifact restore changed history footprint")
			}
		})
	}
}

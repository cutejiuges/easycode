package openai

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestCheckpointWindowAndRestoredRequest(t *testing.T) {
	auto, _ := domain.NewAutoCompactionScope("01900000-0000-7000-8000-000000000099", 1)
	for _, scope := range []domain.CompactionScope{domain.ManualCompactionScope(), auto} {
		for _, key := range []domain.OpenAICacheKeyMode{domain.OpenAICacheOmit, domain.OpenAICacheThread} {
			policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, key, domain.OpenAIHTTPSSE)
			t.Run(string(scope.Trigger)+"/"+string(key), func(t *testing.T) { testCheckpointWindowAndRestoredRequest(t, scope, policy) })
		}
	}
}

func testCheckpointWindowAndRestoredRequest(t *testing.T, scope domain.CompactionScope, policy domain.CachePolicy) {
	instance, err := New(Config{CachePolicy: policy, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	entries := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem(strings.Repeat("archived work ", 100))), {Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", EncryptedContent: "old-opaque"}, {Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "old answer"}}}}}})
	envelopes := make([]provider.NativeCommitEnvelope, 0)
	for _, entry := range entries {
		conversation.history.Append(entry)
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, envelope)
	}
	records := testRestoreRecords(t, envelopes)
	input, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	compactInput, err := provider.NewCompactionInput(input, "preserve test failures", 4096)
	if err != nil {
		t.Fatal(err)
	}
	request, err := conversation.compileCompactionRequest(conversation.history.Snapshot(), compactInput)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := "testdata/compaction_request.golden.json"
	if policy != domain.DefaultCachePolicy() {
		goldenPath = "testdata/cache_thread_compaction_request.golden.json"
	}
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(request.Bytes(), bytes.TrimSpace(golden)) {
		t.Fatal("compaction request golden mismatch")
	}

	var wire responsesRequest
	if err := codec.Unmarshal(request.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.ToolChoice != "none" || wire.MaxOutputTokens != 4096 || len(wire.Tools) != 10 || !strings.Contains(string(request.Bytes()), "preserve test failures") {
		t.Fatal("summary request protection or sources missing")
	}
	testsupport.CheckCheckpointWindow(t, conversation, scope, input, compactInput, records, func(prepared *provider.PreparedCompaction, checkpoint provider.CheckpointEnvelope) {
		testOfflineUsageEvidence(t, instance, prepared, checkpoint)
	})
}

func assertCompactionAfterToolSteering(t *testing.T, instance *Provider, conversation *Conversation, sampling provider.TurnInput, envelopes []provider.NativeCommitEnvelope, outputs []tool.ModelOutput) {
	t.Helper()
	records := testRestoreRecords(t, envelopes, outputs)
	records = testsupport.CompactToolHistory(t, conversation, sampling, records)

	// 两次压缩之后，工具闭环的同轮 steering 仍按原生顺序追加。
	initial := testInputEntry(t, NewUserItem("post checkpoint input"))
	tail := []nativeHistoryEntry{initial, nativeHistoryEntry{Kind: nativeHistorySample, TurnID: initial.TurnID, Outputs: []NativeItem{{Type: "function_call", ID: "fc-next", CallID: "call-next", Name: "Read", Arguments: `{"file_path":"next"}`}}}, nativeHistoryEntry{Kind: nativeHistoryToolOutputs, TurnID: initial.TurnID, ToolOutputs: []NativeItem{{Type: "function_call_output", CallID: "call-next", Output: `{"status":"success","code":"ok","content":"done"}`}}}}
	for _, entry := range tail {
		conversation.history.Append(entry)
	}
	inputID := domain.InputID("01900000-0000-7000-8000-000000000090")
	association, _ := domain.NewInputAssociation(initial.TurnID, domain.InputSteering, 1, []domain.InputID{inputID})
	value, _ := domain.NewUserInput(inputID, "post checkpoint steering")
	prepared, err := conversation.PrepareUserInputs(association, []domain.UserInput{value})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	entries := conversation.history.Entries()
	tail = append(tail, entries[len(entries)-1])
	var tailEnvelopes []provider.NativeCommitEnvelope
	for _, entry := range tail {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		tailEnvelopes = append(tailEnvelopes, envelope)
	}
	records = append(records, testRestoreRecords(t, tailEnvelopes, []tool.ModelOutput{testsupport.ReadOutput(t, "call-next", "next", "done")})...)
	restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	live, err := conversation.compileSamplingRequest(conversation.history.Entries(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.compileSamplingRequest(restored.history.Entries(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live.Bytes(), recovered.Bytes()) || !bytes.Contains(live.Bytes(), []byte("post checkpoint steering")) {
		t.Fatal("post-checkpoint steering request changed")
	}
	liveTranscript, _, _ := conversation.ProjectTranscriptFrom(0)
	restoredTranscript, _, _ := restored.ProjectTranscriptFrom(0)
	if !reflect.DeepEqual(liveTranscript, restoredTranscript) {
		t.Fatal("post-checkpoint native ordinal changed")
	}
}

package anthropic

import (
	"bytes"
	"reflect"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

// taskCacheFixture 复用真实 Task 输入与结果契约，保留原生 thinking/redacted 顺序。
func taskCacheFixture(t *testing.T, instance *Provider) (*Conversation, []provider.RestoreRecord, domain.TaskProgressState, provider.TurnInput) {
	t.Helper()
	c := testConversation(t, instance)
	entries := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage("make a plan")), {
		Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
			{Type: blockTypeThinking, Thinking: "private task reasoning", Signature: "opaque-task-cache"},
			{Type: blockTypeRedactedThinking, RedactedData: "opaque-task-redacted"},
			{Type: blockTypeToolUse, ID: "call-task", Name: "TodoWrite", Input: []byte(taskNativeInput)},
		}}, Metadata: messageMetadata{ID: "task", Model: instance.config.Model},
	}})
	for _, entry := range entries {
		c.history.Append(entry)
	}
	input, err := tool.DecodeTodoWriteInput([]byte(taskNativeInput))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := tool.NewTaskReadyCall("call-task", input)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewInvocation("01900000-0000-7000-8000-000000000002", ready)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := invocation.Task()
	result, err := tool.NewTaskInvocationResult(task)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := tool.NewExecutionAdmission(invocation.InvocationID(), 3)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []tool.ModelOutput{testsupport.DirectModelOutput(t, invocation, result, admission)}
	prepared, err := c.PrepareToolOutputs(outputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	var records []provider.RestoreRecord
	for _, entry := range c.history.Entries() {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		var record provider.RestoreRecord
		switch entry.Kind {
		case nativeHistoryUserInputs:
			association, _ := entry.InputMetadata.Association()
			record, err = provider.NewInputRestoreRecord(association, envelope)
		case nativeHistorySample:
			record, err = provider.NewSampleRestoreRecord(entry.TurnID, envelope)
			if err == nil {
				record, err = record.WithReadyFacts([]tool.Invocation{invocation}, []tool.InvocationResult{result})
			}
		case nativeHistoryToolOutputs:
			record, err = provider.NewToolOutputRestoreRecord(entry.TurnID, envelope, outputs)
		}
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	source, err := domain.NewTaskProgressSource(5, entries[0].TurnID, string(invocation.InvocationID()))
	if err != nil {
		t.Fatal(err)
	}
	state, err := domain.NewTaskProgressState(source, input.Update())
	if err != nil {
		t.Fatal(err)
	}
	sampling, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	return c, records, state, sampling
}

func assertTaskCacheEquivalent(t *testing.T, left, right *Conversation, input provider.TurnInput) codec.CanonicalJSON {
	t.Helper()
	a, err := left.compileSamplingRequest(left.history.Entries(), input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := right.compileSamplingRequest(right.history.Entries(), input)
	if err != nil || !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("task cache restore changed complete canonical request", err)
	}
	nativeA, err := codec.MarshalStable(left.history.Entries())
	if err != nil {
		t.Fatal(err)
	}
	nativeB, err := codec.MarshalStable(right.history.Entries())
	if err != nil || !bytes.Equal(nativeA, nativeB) || !bytes.Contains(nativeA, []byte("opaque-task-cache")) || !bytes.Contains(nativeA, []byte("opaque-task-redacted")) {
		t.Fatal("task cache restore changed native opaque bytes or item order", err)
	}
	evidenceA, err := left.TestEvidence(input)
	if err != nil {
		t.Fatal(err)
	}
	evidenceB, err := right.TestEvidence(input)
	if err != nil || evidenceA.NativeFingerprint() != evidenceB.NativeFingerprint() || !bytes.Equal(evidenceA.Properties().Bytes(), evidenceB.Properties().Bytes()) || left.TestCacheSources(t) != right.TestCacheSources(t) {
		t.Fatal("task cache restore changed request or source fingerprints", err)
	}
	markersA, err := requestMarkerCount(a, left.provider.config.CachePolicy.Anthropic())
	if err != nil {
		t.Fatal(err)
	}
	receiptA, err := left.describeRequest(compiledMessagesRequest{body: a, markers: markersA}, evidenceA, domain.RequestSample)
	if err != nil {
		t.Fatal(err)
	}
	markersB, err := requestMarkerCount(b, right.provider.config.CachePolicy.Anthropic())
	if err != nil {
		t.Fatal(err)
	}
	receiptB, err := right.describeRequest(compiledMessagesRequest{body: b, markers: markersB}, evidenceB, domain.RequestSample)
	if err != nil || receiptA.RequestFingerprint != receiptB.RequestFingerprint || receiptA.NativeFingerprint != receiptB.NativeFingerprint || receiptA.Markers != receiptB.Markers {
		t.Fatal("task cache restore changed receipt fingerprints or markers", err)
	}
	return a
}

func TestTaskCacheTTLNativeCheckpointAndRestore(t *testing.T) {
	mode := domain.AnthropicCache5m

	policy, err := domain.NewCachePolicy(mode, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "claude-test", CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	live, records, state, input := taskCacheFixture(t, instance)
	value, err := testRestoreAfterPreview(t, instance, live.threadID, records)
	if err != nil {
		t.Fatal(err)
	}
	wire := assertTaskCacheEquivalent(t, live, value.(*Conversation), input)
	markers, err := requestMarkerCount(wire, mode)
	if err != nil || (mode == domain.AnthropicCacheDisabled && markers != 0) || (mode != domain.AnthropicCacheDisabled && markers != 2) {
		t.Fatal("task result tail violated TTL marker budget", markers, err)
	}
	if (mode == domain.AnthropicCache1h) != bytes.Contains(wire.Bytes(), []byte(`"ttl":"1h"`)) {
		t.Fatal("task request changed TTL behavior")
	}
	var request struct {
		Messages []struct {
			Content []struct {
				Type         string        `json:"type"`
				CacheControl *cacheControl `json:"cache_control"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := codec.Unmarshal(wire.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	tail := request.Messages[len(request.Messages)-1].Content[0]
	if tail.Type != blockTypeToolResult || (tail.CacheControl != nil) != (mode != domain.AnthropicCacheDisabled) || bytes.Count(wire.Bytes(), []byte("检查中文代码")) != 1 {
		t.Fatal("cache overlay misplaced or duplicated the task result")
	}
	before, _ := codec.MarshalStable(live.history.Entries())
	transcript, _, err := live.ProjectTranscriptFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := provider.NewCompactionInput(input, "retain exact task progress", 4096)
	if err != nil {
		t.Fatal(err)
	}
	compact, err = compact.WithTaskProgress(state)
	if err != nil {
		t.Fatal(err)
	}
	preview, _, err := live.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	generation := compactionTestGeneration("goal; next action", "task-summary-opaque")
	prepared, err := live.prepareCheckpoint(domain.ManualCompactionScope(), "01900000-0000-7000-8000-000000000099", preview.Source(), generation, syntheticRequestReceipt(domain.RequestSummary), state)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := prepared.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	record, err := provider.NewCheckpointRestoreRecord(checkpoint, state)
	if err != nil {
		t.Fatal(err)
	}
	value, err = testRestoreAfterPreview(t, instance, live.threadID, append(records, record))
	if err != nil {
		t.Fatal(err)
	}
	wire = assertTaskCacheEquivalent(t, live, value.(*Conversation), input)
	after, _ := codec.MarshalStable(live.history.Entries())
	restoredTranscript, _, err := value.(*Conversation).ProjectTranscriptFrom(0)
	if err != nil || !bytes.Equal(before, after) || !reflect.DeepEqual(transcript, restoredTranscript) || bytes.Count(wire.Bytes(), []byte("检查中文代码")) != 1 || bytes.Contains(wire.Bytes(), []byte("result_sequence")) {
		t.Fatal("task checkpoint lost frozen history or duplicated prompt state", err)
	}
	markers, err = requestMarkerCount(wire, mode)
	if err != nil || (mode == domain.AnthropicCacheDisabled && markers != 0) || (mode != domain.AnthropicCacheDisabled && markers != 2) || (mode == domain.AnthropicCache1h) != bytes.Contains(wire.Bytes(), []byte(`"ttl":"1h"`)) {
		t.Fatal("task checkpoint changed TTL overlay", err)
	}

}

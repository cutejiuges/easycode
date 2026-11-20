package openai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"github.com/coder/websocket"
)

func taskCacheRecords(t *testing.T, c *Conversation, invocation tool.Invocation, result tool.InvocationResult, outputs []tool.ModelOutput) []provider.RestoreRecord {
	t.Helper()
	var records []provider.RestoreRecord
	for _, entry := range c.history.snapshot() {
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
			if err == nil && len(functionCalls(entry.Outputs)) != 0 {
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
	return records
}

func taskCacheState(t *testing.T, c *Conversation, invocation tool.Invocation) domain.TaskProgressState {
	t.Helper()
	task, ok := invocation.Task()
	if !ok {
		t.Fatal("task invocation missing")
	}
	source, err := domain.NewTaskProgressSource(5, c.history.snapshot()[0].TurnID, string(invocation.InvocationID()))
	if err != nil {
		t.Fatal(err)
	}
	state, err := domain.NewTaskProgressState(source, task.Input().Update())
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func taskCacheOutput(t *testing.T, invocation tool.Invocation) (tool.InvocationResult, []tool.ModelOutput) {
	t.Helper()
	task, ok := invocation.Task()
	if !ok {
		t.Fatal("task invocation missing")
	}
	result, err := tool.NewTaskInvocationResult(task)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := tool.NewExecutionAdmission(invocation.InvocationID(), 3)
	if err != nil {
		t.Fatal(err)
	}
	return result, []tool.ModelOutput{testDirectModelOutput(t, invocation, result, admission)}
}

func assertTaskCacheEquivalent(t *testing.T, left, right *Conversation, input provider.TurnInput) codec.CanonicalJSON {
	t.Helper()
	a, err := left.compileSamplingRequest(left.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := right.compileSamplingRequest(right.history.snapshot(), input)
	if err != nil || !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("task cache restore changed complete canonical request", err)
	}
	nativeA, err := codec.MarshalStable(left.history.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	nativeB, err := codec.MarshalStable(right.history.snapshot())
	if err != nil || !bytes.Equal(nativeA, nativeB) || !bytes.Contains(nativeA, []byte("opaque-task-cache")) || !bytes.Contains(nativeA, []byte(`"phase":"commentary"`)) {
		t.Fatal("task cache restore changed native opaque bytes, phase or item order", err)
	}
	evidenceA, err := left.requestEvidence(input)
	if err != nil {
		t.Fatal(err)
	}
	evidenceB, err := right.requestEvidence(input)
	if err != nil || evidenceA.NativeFingerprint() != evidenceB.NativeFingerprint() || !bytes.Equal(evidenceA.Properties().Bytes(), evidenceB.Properties().Bytes()) || websocketCacheSources(t, left) != websocketCacheSources(t, right) {
		t.Fatal("task cache restore changed native or source fingerprints", err)
	}
	receiptA, err := left.describeRequest(a, evidenceA, domain.RequestSample)
	if err != nil {
		t.Fatal(err)
	}
	receiptB, err := right.describeRequest(b, evidenceB, domain.RequestSample)
	if err != nil || receiptA.RequestFingerprint != receiptB.RequestFingerprint || receiptA.NativeFingerprint != receiptB.NativeFingerprint {
		t.Fatal("task cache restore changed receipt fingerprints", err)
	}
	return a
}

func taskCacheCheckpoint(t *testing.T, c *Conversation, input provider.TurnInput, state domain.TaskProgressState) provider.CheckpointEnvelope {
	t.Helper()
	compact, err := provider.NewCompactionInput(input, "retain exact task progress", 4096)
	if err != nil {
		t.Fatal(err)
	}
	compact, err = compact.WithTaskProgress(state)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := c.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	generation := nativeHistoryEntry{Kind: nativeHistorySample, Outputs: []NativeItem{
		{Type: "reasoning", EncryptedContent: "task-summary-opaque"},
		{Type: "message", Role: "assistant", Phase: "final_answer", Content: []ContentPart{{Type: "output_text", Text: "goal; next action"}}},
	}}
	prepared, err := c.prepareCheckpoint(domain.ManualCompactionScope(), "01900000-0000-7000-8000-000000000099", preview.Source(), generation, syntheticRequestReceipt(domain.RequestSummary), state)
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
	return checkpoint
}

func TestTaskCacheWebSocketSuffixCheckpointFailureAndRestore(t *testing.T) {
	key := domain.OpenAICacheThread

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	requests := make(chan []byte, 8)
	var creates, connects, posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		connects.Add(1)
		for {
			_, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			n := creates.Add(1)
			requests <- body
			if n == 4 {
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"error","error":{"code":"previous_response_not_found","message":"private task error"}}`))
				return
			}
			events := []string{fmt.Sprintf(`{"type":"response.created","response":{"id":"task-cache-%d"}}`, n)}
			if n == 1 {
				events = append(events,
					`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"r-task","encrypted_content":"opaque-task-cache","future":{"task":true}}}`,
					`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Working on the task"}]}}`,
					`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"item-task","call_id":"call-task","name":"update_plan"}}`,
					fmt.Sprintf(`{"type":"response.function_call_arguments.done","output_index":2,"item_id":"item-task","arguments":%q}`, taskNativeInput),
					fmt.Sprintf(`{"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"item-task","call_id":"call-task","name":"update_plan","arguments":%q}}`, taskNativeInput))
			} else {
				events = append(events, `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"next action"}]}}`)
			}
			events = append(events, fmt.Sprintf(`{"type":"response.completed","response":{"id":"task-cache-%d"}}`, n))
			for _, event := range events {
				if err := conn.Write(r.Context(), websocket.MessageText, []byte(event)); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	policy, err := domain.NewCachePolicy(domain.AnthropicCacheDisabled, key, domain.OpenAIWebSocketAuto)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "gpt-test", CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	live := testConversation(t, instance)
	defer live.closeWebsocket()
	input := testOpenAITurnInput(t, live, "make a plan")
	initial, err := live.compileSamplingRequest(live.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := live.Stream(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	first := readOpenAITerminal(t, live, stream)
	if first.Kind() != provider.StreamEventCompleted {
		t.Fatal(first.Error())
	}
	firstWire := <-requests
	full, err := websocketBody(initial, "", 0)
	if err != nil || !bytes.Equal(firstWire, full.Bytes()) || live.wsResponseID != "" {
		t.Fatal("task create did not send full canonical input or advanced before durable sample", err)
	}
	ready, err := first.PreparedSample().ReadyCalls()
	if err != nil || len(ready) != 1 || ready[0].Capability() != tool.CapabilityTaskProgress {
		t.Fatal("task ready fixture missing", err)
	}
	invocation, err := tool.NewInvocation("01900000-0000-7000-8000-000000000002", ready[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := first.PreparedSample().Finalize(); err != nil {
		t.Fatal(err)
	}
	result, outputs := taskCacheOutput(t, invocation)
	preparedOutput, err := live.PrepareToolOutputs(outputs)
	if err != nil {
		t.Fatal(err)
	}
	if err := preparedOutput.Finalize(); err != nil {
		t.Fatal(err)
	}
	input, err = testSamplingInput(t, instance.config.Inference).WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	logical, err := live.compileSamplingRequest(live.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	stream, err = live.Stream(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second := readOpenAITerminal(t, live, stream)
	if second.Kind() != provider.StreamEventCompleted {
		t.Fatal(second.Error())
	}
	var prefix, suffix websocketCreate
	suffixWire := <-requests
	if codec.Unmarshal(firstWire, &prefix) != nil || codec.Unmarshal(suffixWire, &suffix) != nil || suffix.PreviousResponseID != "task-cache-1" || len(suffix.Input) != 1 || suffix.Input[0].Type != "function_call_output" || suffix.Input[0].CallID != "call-task" || !strings.Contains(suffix.Input[0].Output, result.Preview().Text()) || suffix.PromptCacheKey != prefix.PromptCacheKey {
		t.Fatal("task result was not the sole durable incremental suffix")
	}
	var expected responsesRequest
	if err := codec.Unmarshal(logical.Bytes(), &expected); err != nil {
		t.Fatal(err)
	}
	reconstructed := append(append(append([]NativeItem(nil), prefix.Input...), live.history.snapshot()[1].Outputs...), suffix.Input...)
	a, err := codec.MarshalCanonical(reconstructed, maxResponsesRequestBytes)
	if err != nil {
		t.Fatal(err)
	}
	b, err := codec.MarshalCanonical(expected.Input, maxResponsesRequestBytes)
	if err != nil || !bytes.Equal(a.Bytes(), b.Bytes()) || !bytes.Contains(a.Bytes(), []byte(`"future":{"task":true}`)) || !bytes.Contains(a.Bytes(), []byte(`"phase":"commentary"`)) {
		t.Fatal("task incremental reconstruction changed logical input, opaque order or phase", err)
	}
	expectedSuffix, err := websocketBody(logical, "task-cache-1", len(prefix.Input)+len(live.history.snapshot()[1].Outputs))
	if err != nil || !bytes.Equal(suffixWire, expectedSuffix.Bytes()) {
		t.Fatal("task incremental wire changed logical request properties", err)
	}
	rebuilt := suffix
	rebuilt.PreviousResponseID, rebuilt.Input = "", reconstructed
	rebuiltFull, err := codec.MarshalCanonical(rebuilt, maxResponsesRequestBytes)
	if err != nil {
		t.Fatal(err)
	}
	expectedFull, err := websocketBody(logical, "", 0)
	if err != nil || !bytes.Equal(rebuiltFull.Bytes(), expectedFull.Bytes()) {
		t.Fatal("task incremental reconstruction changed complete logical request", err)
	}
	receipt, err := second.PreparedSample().Receipt()
	if err != nil || receipt.Mode != domain.RequestIncremental || receipt.RequestFingerprint != domain.ContentFingerprint(logical.Bytes()) {
		t.Fatal("task incremental receipt did not describe full logical request", err)
	}
	if err := second.PreparedSample().Discard(); err != nil {
		t.Fatal(err)
	}
	stream, err = live.Stream(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	third := readOpenAITerminal(t, live, stream)
	if third.Kind() != provider.StreamEventCompleted {
		t.Fatal(third.Error())
	}
	full, _ = websocketBody(logical, "", 0)
	if actual := <-requests; !bytes.Equal(actual, full.Bytes()) || connects.Load() != 2 {
		t.Fatal("task discard reset lost the complete native request")
	}
	if err := third.PreparedSample().Finalize(); err != nil {
		t.Fatal(err)
	}
	state := taskCacheState(t, live, invocation)
	records := taskCacheRecords(t, live, invocation, result, outputs)
	checkpoint := taskCacheCheckpoint(t, live, input, state)
	record, err := provider.NewCheckpointRestoreRecord(checkpoint, state)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, record)
	// 手动压缩后由后续用户输入继续，不能把已经结束的 assistant sample 伪装成可采样尾部。
	nextInput := testInputEntry(t, NewUserItem("continue preserved task"))
	live.history.commit(nextInput)
	envelope, err := encodeNativeCommit(nextInput)
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope})...)
	value, err := testRestoreAfterPreview(t, instance, live.threadID, records)
	if err != nil {
		t.Fatal(err)
	}
	restored := value.(*Conversation)
	defer restored.closeWebsocket()
	logical = assertTaskCacheEquivalent(t, live, restored, input)
	before, err := live.HistoryFootprint()
	if err != nil {
		t.Fatal(err)
	}
	full, _ = websocketBody(logical, "", 0)
	stream, err = live.Stream(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	failed := readOpenAITerminal(t, live, stream)
	if failed.Kind() != provider.StreamEventFailed || creates.Load() != 4 || posts.Load() != 0 || strings.Contains(failed.Error().Error(), "private") {
		t.Fatal("post-checkpoint connection failure retried or leaked provider text")
	}
	if actual := <-requests; !bytes.Equal(actual, full.Bytes()) {
		t.Fatal("post-checkpoint connection reused an incremental baseline")
	}
	after, err := live.HistoryFootprint()
	if err != nil || before != after {
		t.Fatal("connection failure changed task checkpoint facts", err)
	}
	for _, c := range []*Conversation{live, restored} {
		stream, err = c.Stream(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		completed := readOpenAITerminal(t, c, stream)
		if completed.Kind() != provider.StreamEventCompleted {
			t.Fatal(completed.Error())
		}
		actual := <-requests
		receipt, err := completed.PreparedSample().Receipt()
		if err != nil || !bytes.Equal(actual, full.Bytes()) || receipt.Mode != domain.RequestFull || receipt.RequestFingerprint != domain.ContentFingerprint(logical.Bytes()) || (c == restored && receipt.Reason != domain.ResetRestored) {
			t.Fatal("task checkpoint reconnect/restore changed full canonical request or fingerprint", err)
		}
		if err := completed.PreparedSample().Discard(); err != nil {
			t.Fatal(err)
		}
	}
	// 纯内存 checkpoint 清除增量资格；已有连接可发 full，错误和 discard 才关闭传输。
	if creates.Load() != 6 || connects.Load() != 4 || posts.Load() != 0 {
		t.Fatal("task connection reset used an unexpected retry path", creates.Load(), connects.Load(), posts.Load())
	}

}

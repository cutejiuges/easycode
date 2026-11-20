package anthropic

import (
	"bytes"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"fmt"
	"os"
	"strings"
	"testing"
)

const taskNativeInput = `{"todos":[{"content":"检查中文代码","activeForm":"正在检查","status":"in_progress"}]}`

func TestTaskReducerRequiresCompleteStrictInput(t *testing.T) {
	invalid := []string{
		strings.Replace(taskNativeInput, `"status":"in_progress"`, `"status":null`, 1),
		strings.Replace(taskNativeInput, `"status":"in_progress"`, `"status":"in_progress","status":"pending"`, 1),
		strings.TrimSuffix(taskNativeInput, "}") + `,"unknown":true}`,
		`{"plan":[],"todos":[]}`,
	}
	for _, data := range append([]string{taskNativeInput}, invalid...) {
		t.Run(fmt.Sprint(len(data)), func(t *testing.T) {
			r := newStreamReducer(testAnthropicToolCatalog(t))
			if _, err := reduceAnthropicTestEvent(r, `{"type":"message_start","message":{"id":"task","model":"claude-test"}}`); err != nil {
				t.Fatal(err)
			}
			if _, err := reduceAnthropicTestEvent(r, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-task","name":"TodoWrite","input":{}}}`); err != nil {
				t.Fatal(err)
			}
			var err error
			for _, part := range data {
				_, err = reduceAnthropicTestEvent(r, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%q}}`, string(part)))
				if err != nil {
					break
				}
				if len(r.readyCalls()) != 0 {
					t.Fatal("partial task call became ready")
				}
			}
			if err == nil {
				_, err = reduceAnthropicTestEvent(r, `{"type":"content_block_stop","index":0}`)
			}
			if data != taskNativeInput {
				if err == nil || len(r.readyCalls()) != 0 {
					t.Fatal("invalid task call accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = reduceAnthropicTestEvent(r, `{"type":"message_stop"}`)
			if err != nil {
				t.Fatal(err)
			}
			ready := r.readyCalls()
			if len(ready) != 1 || ready[0].Capability() != tool.CapabilityTaskProgress {
				t.Fatal("completed task ready call missing")
			}
			if got := ready[0].TaskInput().Update().Items()[0].Content(); got != "检查中文代码" {
				t.Fatal("UTF-8 task changed", got)
			}
		})
	}
}

func TestTaskNativeResultsRestoreExactly(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()

	c := testConversation(t, instance)
	frozen := frozenFileInput(t, true)
	association, err := domain.NewInputAssociation(domain.TurnID(testInputID(frozen.Text())), domain.InputInitial, 0, []domain.InputID{frozen.ID()})
	if err != nil {
		t.Fatal(err)
	}
	fileEntry, err := userInputEntry(association, []domain.UserInput{frozen})
	if err != nil || len(fileEntry.InputMetadata.Files) != 1 || fileEntry.InputMetadata.Files[0].Attachments.Len() != 2 {
		t.Fatal("combined file metadata missing", err)
	}
	entries := testAssociatedHistory([]nativeHistoryEntry{fileEntry, nativeHistoryEntry{Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "private reasoning", Signature: "opaque-task"}, {Type: blockTypeRedactedThinking, RedactedData: "opaque-redacted"}, {Type: blockTypeToolUse, ID: "call-task", Name: "TodoWrite", Input: []byte(taskNativeInput)}}}, Metadata: messageMetadata{ID: "task", Model: "claude-test"}}})
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
	inv, err := tool.NewInvocation("01900000-0000-7000-8000-000000000002", ready)
	if err != nil {
		t.Fatal(err)
	}
	task, _ := inv.Task()
	result, err := tool.NewTaskInvocationResult(task)
	if err != nil {
		t.Fatal(err)
	}

	before := c.historySnapshot()
	admission, err := tool.NewExecutionAdmission(inv.InvocationID(), 3)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []tool.ModelOutput{testDirectModelOutput(t, inv, result, admission)}
	preflight, err := c.PrepareToolOutputPreflight(inv)
	if err != nil || preflight.Validate(outputs[0]) != nil {
		t.Fatal("task output preflight failed", err)
	}
	badUpdate, err := tool.DecodeTodoWriteInput([]byte(strings.Replace(taskNativeInput, "检查中文代码", "伪造同长内容", 1)))
	if err != nil {
		t.Fatal(err)
	}
	badReady, _ := tool.NewTaskReadyCall("call-task", badUpdate)
	badInv, _ := tool.NewInvocation(inv.InvocationID(), badReady)
	if _, err := c.PrepareToolOutputPreflight(badInv); err == nil {
		t.Fatal("task preflight accepted forged input")
	}
	entry, err := prepareToolOutputsEntry(before, outputs)
	if err != nil {
		t.Fatal(err)
	}
	entry.TurnID = entries[0].TurnID
	c.history.Append(entry)
	entries = append(entries, entry)
	var records []provider.RestoreRecord
	for index, entry := range entries {
		envelope, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		var record provider.RestoreRecord
		switch index {
		case 0:
			association, _ := entry.InputMetadata.Association()
			record, err = provider.NewInputRestoreRecord(association, envelope)
		case 1:
			record, err = provider.NewSampleRestoreRecord(entry.TurnID, envelope)
			if err == nil {
				record, err = record.WithReadyFacts([]tool.Invocation{inv}, []tool.InvocationResult{result})
			}
		case 2:
			record, err = provider.NewToolOutputRestoreRecord(entry.TurnID, envelope, outputs)
		}
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	restored, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", records)
	if err != nil {
		t.Fatal(err)
	}
	left, err := compileMessagesRequest("claude-test", 4096, c.historySnapshot(), nil, testAnthropicToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	right, err := compileMessagesRequest("claude-test", 4096, restored.(*Conversation).historySnapshot(), nil, testAnthropicToolView(t))
	if err != nil || !bytes.Equal(left.Bytes(), right.Bytes()) {
		t.Fatal("task restore changed canonical request", err)
	}

	fixture, err := os.ReadFile("testdata/task_continuation.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(fixture), left.Bytes()) {
		t.Fatal("combined file and task continuation golden changed")
	}
	if strings.Count(string(left.Bytes()), "检查中文代码") != 1 {
		t.Fatal("ordinary task call was duplicated")
	}

	_, a, _ := c.ProjectContextWindow()
	_, b, _ := restored.(*Conversation).ProjectContextWindow()
	if a != b {
		t.Fatal("task restore changed footprint")
	}
	missingResult, err := provider.NewSampleRestoreRecord(entries[1].TurnID, func() provider.NativeCommitEnvelope { v, _ := encodeNativeCommit(entries[1]); return v }())
	if err != nil {
		t.Fatal(err)
	}
	missingResult, err = missingResult.WithReadyFacts([]tool.Invocation{inv}, nil)
	if err != nil {
		t.Fatal(err)
	}
	without := append([]provider.RestoreRecord(nil), records...)
	without[1] = missingResult
	if _, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", without); err == nil {
		t.Fatal("native task confirmation lacked frozen result evidence")
	}
	badRecord, _ := provider.NewSampleRestoreRecord(entries[1].TurnID, func() provider.NativeCommitEnvelope { v, _ := encodeNativeCommit(entries[1]); return v }())
	badRecord, err = badRecord.WithReadyFacts([]tool.Invocation{badInv}, nil)
	if err != nil {
		t.Fatal(err)
	}
	forged := append([]provider.RestoreRecord(nil), records...)
	forged[1] = badRecord
	if _, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", forged); err == nil {
		t.Fatal("restored a forged task input")
	}

}

// testTaskRestoreResults 只为成功原生测试数据构造配对事实，错误必须由测试显式提供。
func testTaskRestoreResults(t *testing.T, following []provider.NativeCommitEnvelope, tasks []tool.TaskInvocation) []tool.InvocationResult {
	t.Helper()
	if len(tasks) == 0 {
		return nil
	}
	for _, envelope := range following {
		entry, err := decodeNativeCommit(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Kind != nativeHistoryToolOutputs {
			break
		}
		var results []tool.InvocationResult
		for _, task := range tasks {
			for _, item := range entry.ToolOutputs.Content {
				if item.ToolUseID != string(task.ProviderCallID()) {
					continue
				}
				if item.IsError {
					t.Fatal("task fixture requires explicit failure facts")
				}
				preview := item.Content
				result, err := tool.NewTaskInvocationResult(task)
				if err != nil || result.Preview().Text() != preview {
					t.Fatal("task confirmation differs", err)
				}
				results = append(results, result)
			}
		}
		return results
	}
	return nil
}

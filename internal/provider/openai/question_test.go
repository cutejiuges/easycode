package openai

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const questionNativeInput = `{"questions":[{"id":"storage","header":"存储","question":"选择存储方式？","options":[{"label":"SQLite","description":"本地"},{"label":"Remote","description":"远程"}],"multi_select":false}]}`

func TestQuestionReducerStrictTerminal(t *testing.T) {
	for _, data := range []string{questionNativeInput, strings.Replace(questionNativeInput, `"questions":`, `"answers":[],"questions":`, 1), strings.Replace(questionNativeInput, `"multi_select":false`, `"multi_select":null`, 1), strings.Replace(questionNativeInput, `"id":"storage"`, `"id":"storage","id":"other"`, 1)} {
		t.Run(fmt.Sprint(len(data)), func(t *testing.T) {
			r := newResponsesStreamReducer(testOpenAIToolCatalog(t))
			reduceOpenAIEvent(t, r, `{"type":"response.created","response":{"id":"question"}}`)
			reduceOpenAIEvent(t, r, `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item-q","call_id":"call-q","name":"request_user_input"}}`)
			var err error
			for _, part := range data {
				_, err = r.reduce(transport.SSEEvent{Data: fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":"item-q","output_index":0,"delta":%q}`, string(part))})
				if err != nil {
					break
				}
				if len(r.readyCalls()) != 0 {
					t.Fatal("partial question ready")
				}
			}
			if err == nil {
				_, err = r.reduce(transport.SSEEvent{Data: fmt.Sprintf(`{"type":"response.function_call_arguments.done","item_id":"item-q","output_index":0,"arguments":%q}`, data)})
			}
			if err == nil {
				_, err = r.reduce(transport.SSEEvent{Data: fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"item-q","call_id":"call-q","name":"request_user_input","arguments":%q}}`, data)})
			}
			if data != questionNativeInput {
				if err == nil || len(r.readyCalls()) != 0 {
					t.Fatal("invalid question ready")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.terminal {
				t.Fatal("tool item completed entire sample")
			}
			reduceOpenAIEvent(t, r, `{"type":"response.completed","response":{"id":"question"}}`)

			ready := r.readyCalls()
			if len(ready) != 1 || ready[0].Capability() != tool.CapabilityQuestion {
				t.Fatal("question missing")
			}
		})
	}
}
func TestQuestionPreflightNativeRestoreAndForgery(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	entries := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem("ask")), {Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "reasoning", ID: "r-q", EncryptedContent: "opaque-question"}, {Type: "message", ID: "m-q", Role: "assistant", Phase: "commentary", Content: []ContentPart{{Type: "output_text", Text: "Need your choice"}}}, {Type: "function_call", ID: "item-q", CallID: "call-q", Name: "request_user_input", Arguments: questionNativeInput}}}})
	c := &Conversation{provider: instance}
	for _, e := range entries {
		c.history.commit(e)
	}
	input, _ := tool.DecodeQuestionInput([]byte(questionNativeInput))
	ready, _ := tool.NewQuestionReadyCall("call-q", input)
	id, _ := tool.ParseInvocationID("01900000-0000-7000-8000-000000000002")
	inv, _ := tool.NewInvocation(id, ready)
	q, _ := inv.Question()
	a, _ := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{{QuestionID: "storage", SelectedOptions: []int{0}, Text: "  保留说明\n"}})
	result, err := tool.NewQuestionInvocationResult(q, a)
	if err != nil {
		t.Fatal(err)
	}
	before := c.historySnapshot()
	footprint, _ := c.HistoryFootprint()
	p, err := c.PrepareToolOutputPreflight(inv)
	if err != nil || p.Validate(result) != nil {
		t.Fatal("preflight rejected valid answer", err)
	}
	if after, _ := c.HistoryFootprint(); after != footprint || !reflect.DeepEqual(before, c.historySnapshot()) {
		t.Fatal("preflight mutated native history")
	}
	wrongID, _ := tool.ParseInvocationID("01900000-0000-7000-8000-000000000009")
	wrongInv, _ := tool.NewInvocation(wrongID, ready)
	wrongQ, _ := wrongInv.Question()
	wrongResult, _ := tool.NewQuestionInvocationResult(wrongQ, a)
	if p.Validate(wrongResult) == nil {
		t.Fatal("accepted wrong invocation")
	}
	wrongReady, _ := tool.NewQuestionReadyCall("other-call", input)
	wrongInv, _ = tool.NewInvocation(id, wrongReady)
	if _, err := c.PrepareToolOutputPreflight(wrongInv); err == nil {
		t.Fatal("accepted wrong call")
	}
	entry, err := prepareToolOutputsEntry(before, []tool.InvocationResult{result})
	if err != nil {
		t.Fatal(err)
	}
	changed := entry.clone()
	changed.ToolOutputs[0].Output = strings.Replace(changed.ToolOutputs[0].Output, "SQLite", "forged", 1)
	if validateNativeHistory(append(before, changed)) == nil {
		t.Fatal("forged selected label accepted")
	}
	entry.TurnID = entries[0].TurnID
	c.history.commit(entry)
	entries = append(entries, entry)
	var commits []provider.NativeCommitEnvelope
	for _, e := range entries {
		commit, err := encodeNativeCommit(e)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeNativeCommit(commit)
		if err != nil {
			t.Fatal(err)
		}
		again, err := encodeNativeCommit(decoded)
		if err != nil || !bytes.Equal(commit.Payload(), again.Payload()) {
			t.Fatal("native codec changed bytes")
		}
		commits = append(commits, commit)
	}
	restored, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, commits))
	if err != nil {
		t.Fatal(err)
	}
	left, err := compileResponsesRequest("gpt-test", c.historySnapshot(), nil, testOpenAIToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	original := c
	c = restored.(*Conversation)
	right, err := compileResponsesRequest("gpt-test", c.historySnapshot(), nil, testOpenAIToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left.Bytes(), right.Bytes()) {
		t.Fatal("restored request bytes changed")
	}
	restoredEntries := c.historySnapshot()
	for i, e := range restoredEntries {
		commit, err := encodeNativeCommit(e)
		if err != nil || !bytes.Equal(commit.Payload(), commits[i].Payload()) {
			t.Fatal("restored native item order/opaque bytes changed", err)
		}
	}
	aFoot, _ := original.HistoryFootprint()
	bFoot, _ := c.HistoryFootprint()
	if aFoot != bFoot {
		t.Fatal("restored footprint changed")
	}

	if a, b := original.history.revision, c.history.revision; a != b {
		t.Fatal("restored revision changed")
	}
	if err := p.Validate(result); err != nil {
		t.Fatal("preflight borrowed mutable history", err)
	}
	sampling, err := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	assertCompactionAfterToolSteering(t, instance, original, sampling, commits)

}

// testQuestionRestoreResults 将测试原生 fixture 的成功答案绑定为独立共享事实。
func testQuestionRestoreResults(t *testing.T, following []provider.NativeCommitEnvelope, questions []tool.QuestionInvocation) []tool.InvocationResult {
	t.Helper()
	if len(questions) == 0 {
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
		for _, q := range questions {
			for _, item := range entry.ToolOutputs {
				if !(item.CallID == string(q.ProviderCallID())) {
					continue
				}
				var wire functionOutputPayload
				if err := codec.UnmarshalRecord([]byte(item.Output), &wire); err != nil {
					t.Fatal(err)
				}
				if wire.Status != tool.ResultSuccess {
					t.Fatal("question fixture requires explicit failure facts")
				}
				preview := wire.Content
				a, err := tool.DecodeQuestionPreview(q.Input(), []byte(preview))
				if err != nil {
					t.Fatal(err)
				}
				r, err := tool.NewQuestionInvocationResult(q, a)
				if err != nil {
					t.Fatal(err)
				}
				results = append(results, r)
			}
		}
		return results
	}
	return nil
}

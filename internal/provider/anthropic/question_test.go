package anthropic

import (
	"bytes"
	"easycode/internal/provider"
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
			r := newStreamReducer(testAnthropicToolCatalog(t))
			_, err := reduceAnthropicTestEvent(r, `{"type":"message_start","message":{"id":"question","model":"claude-test"}}`)
			if err != nil {
				t.Fatal(err)
			}
			_, err = reduceAnthropicTestEvent(r, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-q","name":"AskUserQuestion","input":{}}}`)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range data {
				_, err = reduceAnthropicTestEvent(r, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%q}}`, string(part)))
				if err != nil {
					break
				}
				if len(r.readyCalls()) != 0 {
					t.Fatal("partial question ready")
				}
			}
			if err == nil {
				_, err = reduceAnthropicTestEvent(r, `{"type":"content_block_stop","index":0}`)
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
			_, err = reduceAnthropicTestEvent(r, `{"type":"message_stop"}`)
			if err != nil {
				t.Fatal(err)
			}

			ready := r.readyCalls()
			if len(ready) != 1 || ready[0].Capability() != tool.CapabilityQuestion {
				t.Fatal("question missing")
			}
		})
	}
}
func TestQuestionPreflightNativeRestoreAndForgery(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "claude-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	entries := testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage("ask")), {Kind: nativeHistorySample, Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeThinking, Thinking: "private", Signature: "opaque-question"}, {Type: blockTypeRedactedThinking, RedactedData: "opaque-redacted"}, {Type: blockTypeToolUse, ID: "call-q", Name: "AskUserQuestion", Input: []byte(questionNativeInput)}}}, Metadata: messageMetadata{ID: "question", Model: "claude-test"}}})
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
	if err != nil || p.Validate(testModelOutputs(t, result)[0]) != nil {
		t.Fatal("preflight rejected valid answer", err)
	}
	if after, _ := c.HistoryFootprint(); after != footprint || !reflect.DeepEqual(before, c.historySnapshot()) {
		t.Fatal("preflight mutated native history")
	}
	wrongID, _ := tool.ParseInvocationID("01900000-0000-7000-8000-000000000009")
	wrongInv, _ := tool.NewInvocation(wrongID, ready)
	wrongQ, _ := wrongInv.Question()
	wrongResult, _ := tool.NewQuestionInvocationResult(wrongQ, a)
	if p.Validate(testModelOutputs(t, wrongResult)[0]) == nil {
		t.Fatal("accepted wrong invocation")
	}
	wrongReady, _ := tool.NewQuestionReadyCall("other-call", input)
	wrongInv, _ = tool.NewInvocation(id, wrongReady)
	if _, err := c.PrepareToolOutputPreflight(wrongInv); err == nil {
		t.Fatal("accepted wrong call")
	}
	entry, err := prepareToolOutputsEntry(before, testModelOutputs(t, result))
	if err != nil {
		t.Fatal(err)
	}
	changed := entry.clone()
	changed.ToolOutputs.Content[0].Content = strings.Replace(changed.ToolOutputs.Content[0].Content, "SQLite", "forged", 1)
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
	restored, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, commits, testModelOutputs(t, result)))
	if err != nil {
		t.Fatal(err)
	}
	left, err := compileMessagesRequest("claude-test", 1024, c.historySnapshot(), nil, testAnthropicToolView(t))
	if err != nil {
		t.Fatal(err)
	}
	original := c
	c = restored.(*Conversation)
	right, err := compileMessagesRequest("claude-test", 1024, c.historySnapshot(), nil, testAnthropicToolView(t))
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
	if err := p.Validate(testModelOutputs(t, result)[0]); err != nil {
		t.Fatal("preflight borrowed mutable history", err)
	}
	sampling, err := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	assertCompactionAfterToolSteering(t, instance, original, sampling, commits, testModelOutputs(t, result))

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
			for _, item := range entry.ToolOutputs.Content {
				if !(item.ToolUseID == string(q.ProviderCallID())) {
					continue
				}
				if item.IsError {
					t.Fatal("question fixture requires explicit failure facts")
				}
				preview := item.Content
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

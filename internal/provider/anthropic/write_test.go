package anthropic

import (
	"easycode/internal/tool"
	"fmt"
	"strings"
	"testing"
)

func TestWriteReducerStrictInputAndNativeBody(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		valid       bool
	}{
		{"valid", `{"file_path":"new/中文","content":"中文\r\n"}`, true},
		{"empty", `{"file_path":"a","content":""}`, true},
		{"missing", `{"file_path":"a"}`, false},
		{"duplicate", `{"file_path":"a","content":"","content":"x"}`, false},
		{"surrogate", `{"file_path":"a","content":"\ud800"}`, false},
		{"null", `{"file_path":"a","content":null}`, false},
		{"unknown", `{"file_path":"a","content":"","other":0}`, false},
		{"overflow", `{"file_path":"a","content":"` + strings.Repeat("x", 1<<20) + `"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newStreamReducer(testAnthropicToolCatalog(t))
			events := []string{`{"type":"message_start","message":{"id":"write","model":"claude-test"}}`, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call-write","name":"Write","input":{}}}`}
			if tt.valid {
				for _, part := range tt.input {
					events = append(events, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%q}}`, string(part)))
				}
			} else {
				events = append(events, fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%q}}`, tt.input))
			}
			events = append(events, `{"type":"content_block_stop","index":0}`)
			var err error
			for i, event := range events {
				_, err = reduceAnthropicTestEvent(r, event)
				if err != nil {
					break
				}
				if i < len(events)-1 && len(r.readyCalls()) != 0 {
					t.Fatal("partial Write produced Ready")
				}
			}
			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
				ready := r.readyCalls()
				if len(ready) != 1 || ready[0].Capability() != tool.CapabilityWrite || ready[0].WireKind() != tool.WireStructured {
					t.Fatal("Write ready shape")
				}
				if _, err := reduceAnthropicTestEvent(r, `{"type":"message_stop"}`); err != nil {
					t.Fatal(err)
				}
				if string(r.assistantMessage().Content[0].Input) != tt.input {
					t.Fatal("native Write body changed")
				}
			} else if err == nil || len(r.readyCalls()) != 0 {
				t.Fatal("非法 Write 产生执行入口", err)
			}
		})
	}
}

func TestWriteOutputPairsOnlyMatchingToolUse(t *testing.T) {
	input, _ := tool.NewWriteInput("a", "secret-body")
	ready, _ := tool.NewWriteReadyCall("call-write", input)
	id, _ := tool.GenerateInvocationID()
	v, _ := tool.NewWriteInvocation(id, ready)
	result := tool.NewWriteErrorResult(v, tool.ResultDenied, "approval_required", "Write denied: approval_required")
	history := []nativeHistoryEntry{{Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("write")), Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{{Type: blockTypeToolUse, ID: "call-write", Name: "Write", Input: []byte(`{"file_path":"a","content":"secret-body"}`)}}}, Metadata: messageMetadata{ID: "write", Model: "claude-test"}}}
	entry, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.ToolOutputs.Content) != 1 || entry.ToolOutputs.Content[0].Type != blockTypeToolResult || !entry.ToolOutputs.Content[0].IsError || strings.Contains(entry.ToolOutputs.Content[0].Content, "secret-body") {
		t.Fatal("Write output wire/body")
	}
	if err := validateNativeHistory(append(history, entry)); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*NativeItem){func(i *NativeItem) { i.ID = "other" }, func(i *NativeItem) { i.Name = "Read" }} {
		changed := history[0]
		changed.Assistant.Content = append([]NativeItem(nil), history[0].Assistant.Content...)
		mutation(&changed.Assistant.Content[0])
		if _, err := prepareToolOutputsEntry([]nativeHistoryEntry{changed}, []tool.InvocationResult{result}); err == nil {
			t.Fatal("接受错配 output")
		}
	}
}

func TestWriteAggregateNativeAndRequestBudgetsRemainSixteenMiB(t *testing.T) {
	input := `{"file_path":"new/a","content":"` + strings.Repeat("x", (1<<20)-100) + `"}`
	if _, err := tool.DecodeWriteInput([]byte(input)); err != nil {
		t.Fatal("单调用应在预算内", err)
	}
	entry := nativeHistoryEntry{Kind: nativeHistorySample, Input: nativeMessagePointer(newUserMessage("write")), Assistant: nativeMessage{Role: roleAssistant}, Metadata: messageMetadata{ID: "write", Model: "claude-test"}}
	for i := 0; i < 17; i++ {
		entry.Assistant.Content = append(entry.Assistant.Content, NativeItem{Type: blockTypeToolUse, ID: fmt.Sprintf("call-%d", i), Name: "Write", Input: []byte(input)})
	}
	if _, err := encodeNativeCommit(entry); err == nil {
		t.Fatal("聚合 Write 绕过 native record budget")
	}
	if _, err := compileMessagesRequest("claude-test", 1024, []nativeHistoryEntry{entry}, nil, nil, testAnthropicToolView(t)); err == nil {
		t.Fatal("聚合 Write 绕过 request budget")
	}
	if maxMessagesRequestBytes != 16<<20 {
		t.Fatal("请求预算被扩大")
	}
}

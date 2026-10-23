package openai

import (
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
	"fmt"
	"strings"
	"testing"
)

func TestWriteReducerStrictArgumentsAndIdentity(t *testing.T) {
	valid := `{"file_path":"new/中文","content":"中文\r\n"}`
	for _, tt := range []struct {
		name, input, kind, finalName, finalID string
		valid                                 bool
	}{
		{"valid", valid, "function_call", "Write", "call-write", true},
		{"empty", `{"file_path":"a","content":""}`, "function_call", "Write", "call-write", true},
		{"missing", `{"file_path":"a"}`, "function_call", "Write", "call-write", false},
		{"duplicate", `{"file_path":"a","content":"","content":"x"}`, "function_call", "Write", "call-write", false},
		{"surrogate", `{"file_path":"a","content":"\ud800"}`, "function_call", "Write", "call-write", false},
		{"null", `{"file_path":"a","content":null}`, "function_call", "Write", "call-write", false},
		{"unknown", `{"file_path":"a","content":"","other":0}`, "function_call", "Write", "call-write", false},
		{"overflow", `{"file_path":"a","content":"` + strings.Repeat("x", 1<<20) + `"}`, "function_call", "Write", "call-write", false},
		{"name-mismatch", valid, "function_call", "Read", "call-write", false},
		{"id-mismatch", valid, "function_call", "Write", "other", false},
		{"custom-write", valid, "custom_tool_call", "Write", "call-write", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newResponsesStreamReducer(testOpenAIToolCatalog(t))
			reduceOpenAIEvent(t, r, `{"type":"response.created","response":{"id":"write"}}`)
			events := []string{fmt.Sprintf(`{"type":"response.output_item.added","output_index":0,"item":{"type":%q,"id":"item-write","call_id":"call-write","name":"Write"}}`, tt.kind)}
			if tt.kind == "function_call" {
				if tt.valid {
					for _, part := range tt.input {
						events = append(events, fmt.Sprintf(`{"type":"response.function_call_arguments.delta","item_id":"item-write","output_index":0,"delta":%q}`, string(part)))
					}
				}
				events = append(events, fmt.Sprintf(`{"type":"response.function_call_arguments.done","item_id":"item-write","output_index":0,"arguments":%q}`, tt.input), fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"item-write","call_id":%q,"name":%q,"arguments":%q}}`, tt.finalID, tt.finalName, tt.input))
			} else {
				events = append(events, fmt.Sprintf(`{"type":"response.custom_tool_call_input.done","item_id":"item-write","output_index":0,"input":%q}`, tt.input), fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","id":"item-write","call_id":"call-write","name":"Write","input":%q}}`, tt.input))
			}
			var err error
			for i, event := range events {
				_, err = r.reduce(transport.SSEEvent{Data: event})
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
				reduceOpenAIEvent(t, r, `{"type":"response.completed","response":{"id":"write"}}`)
			} else if err == nil || len(r.readyCalls()) != 0 {
				t.Fatal("非法 Write 产生执行入口", err)
			}
		})
	}
}

func TestWriteOutputPairsOnlyMatchingStructuredCall(t *testing.T) {
	input, _ := tool.NewWriteInput("a", "secret-body")
	ready, _ := tool.NewWriteReadyCall("call-write", input)
	id, _ := tool.GenerateInvocationID()
	v, _ := tool.NewWriteInvocation(id, ready)
	result := tool.NewWriteErrorResult(v, tool.ResultDenied, "approval_required", "Write denied: approval_required")
	history := []nativeHistoryEntry{{Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("write")), Outputs: []NativeItem{{Type: "function_call", ID: "item-write", CallID: "call-write", Name: "Write", Arguments: `{"file_path":"a","content":"secret-body"}`}}}}
	entry, err := prepareToolOutputsEntry(history, []tool.InvocationResult{result})
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.ToolOutputs) != 1 || entry.ToolOutputs[0].Type != "function_call_output" || strings.Contains(entry.ToolOutputs[0].Output, "secret-body") {
		t.Fatal("Write output wire/body")
	}
	if err := validateNativeHistory(append(history, entry)); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*NativeItem){func(i *NativeItem) { i.CallID = "other" }, func(i *NativeItem) { i.Name = "Read" }, func(i *NativeItem) { i.Type = "custom_tool_call"; i.Input = i.Arguments; i.Arguments = "" }} {
		changed := history[0]
		changed.Outputs = append([]NativeItem(nil), history[0].Outputs...)
		mutation(&changed.Outputs[0])
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
	entry := nativeHistoryEntry{Kind: nativeHistorySample, Input: nativeItemPointer(NewUserItem("write"))}
	for i := 0; i < 17; i++ {
		entry.Outputs = append(entry.Outputs, NativeItem{Type: "function_call", ID: fmt.Sprintf("item-%d", i), CallID: fmt.Sprintf("call-%d", i), Name: "Write", Arguments: input})
	}
	if _, err := encodeNativeCommit(entry); err == nil {
		t.Fatal("聚合 Write 绕过 native record budget")
	}
	if _, err := compileResponsesRequest("gpt-test", []nativeHistoryEntry{entry}, nil, nil, testOpenAIToolView(t)); err == nil {
		t.Fatal("聚合 Write 绕过 request budget")
	}
	if maxResponsesRequestBytes != 16<<20 {
		t.Fatal("请求预算被扩大")
	}
}

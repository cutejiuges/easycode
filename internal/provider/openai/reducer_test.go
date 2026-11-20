package openai

import (
	"errors"
	"fmt"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
)

func TestResponsesStreamReducerAcceptsOrderedSample(t *testing.T) {
	reducer := newResponsesStreamReducer()
	result := reduceOpenAIEvent(t, reducer, `{"type":"response.created","response":{"id":"resp-1"}}`)
	if result.completed || reducer.responseIdentity() != "resp-1" {
		t.Fatalf("created result/state = %#v/%q", result, reducer.responseIdentity())
	}
	result = reduceOpenAIEvent(t, reducer, `{"type":"response.future.delta","delta":"ignored"}`)
	if result.semantic != nil || result.native != nil || result.completed {
		t.Fatalf("unknown active event result = %#v", result)
	}
	result = reduceOpenAIEvent(t, reducer, `{"type":"response.output_text.delta","delta":"hello"}`)
	if result.semantic == nil {
		t.Fatal("missing semantic event")
	}
	payload, err := protocol.DecodeAssistantTextDelta(*result.semantic)
	if err != nil || payload.Text != "hello" {
		t.Fatalf("text payload = %#v, %v", payload, err)
	}
	result = reduceOpenAIEvent(t, reducer, `{"type":"response.output_item.done","item":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"hello"}],"future":true}}`)
	if result.native == nil || result.native.ID != "msg-1" || len(result.native.Raw) == 0 {
		t.Fatalf("native result = %#v", result.native)
	}
	result = reduceOpenAIEvent(t, reducer, `{"type":"response.completed","response":{"id":"resp-1"}}`)
	if !result.completed || !reducer.terminal || len(reducer.outputItems()) != 1 {
		t.Fatalf("completed result/state = %#v/%#v", result, reducer)
	}
	items := reducer.outputItems()
	items[0].ID = "mutated"
	if reducer.outputItems()[0].ID != "msg-1" {
		t.Fatal("reducer output item getter shares mutable state")
	}
}

func TestResponsesStreamReducerNormalizesCompletedUsage(t *testing.T) {
	reducer := newResponsesStreamReducer()
	reduceOpenAIEvent(t, reducer, `{"type":"response.created","response":{"id":"resp-1"}}`)
	result := reduceOpenAIEvent(t, reducer, `{"type":"response.completed","response":{"id":"resp-1","usage":{"input_tokens":120,"input_tokens_details":{"cached_tokens":40},"cache_write_tokens":5,"output_tokens":30,"output_tokens_details":{"reasoning_tokens":10}}}}`)
	if !result.completed {
		t.Fatal("completion was not accepted")
	}
	usage, err := reducer.sampleUsage()
	if err != nil {
		t.Fatal(err)
	}
	assertOpenAIMetric(t, usage.InputUncached(), domain.UsageMetricKnown, 80, true)
	assertOpenAIMetric(t, usage.CacheRead(), domain.UsageMetricKnown, 40, true)
	assertOpenAIMetric(t, usage.CacheWrite(), domain.UsageMetricKnown, 5, true)
	assertOpenAIMetric(t, usage.Output(), domain.UsageMetricKnown, 30, true)
	assertOpenAIMetric(t, usage.ReasoningOutput(), domain.UsageMetricKnown, 10, true)
}

func TestResponsesStreamReducerProducesOrderedHeterogeneousReadyCalls(t *testing.T) {
	reducer := newResponsesStreamReducer(testOpenAIToolCatalog(t))
	reduceOpenAIEvent(t, reducer, `{"type":"response.created","response":{"id":"resp-tools"}}`)
	calls := []struct {
		index     int
		itemID    string
		callID    string
		name      string
		arguments string
	}{
		{0, "fc-0", "call-0", "Glob", `{"pattern":"**/*.go"}`},
		{1, "fc-1", "call-1", "Grep", `{"pattern":"TODO","glob":"**/*.go"}`},
		{2, "fc-2", "call-2", "Read", `{"file_path":"README.md"}`},
	}
	for _, call := range calls {
		reduceOpenAIEvent(t, reducer, fmt.Sprintf(`{"type":"response.output_item.added","output_index":%d,"item":{"type":"function_call","id":"%s","call_id":"%s","name":"%s"}}`, call.index, call.itemID, call.callID, call.name))
		reduceOpenAIEvent(t, reducer, fmt.Sprintf(`{"type":"response.function_call_arguments.done","item_id":"%s","output_index":%d,"arguments":%q}`, call.itemID, call.index, call.arguments))
		reduceOpenAIEvent(t, reducer, fmt.Sprintf(`{"type":"response.output_item.done","output_index":%d,"item":{"type":"function_call","id":"%s","call_id":"%s","name":"%s","arguments":%q}}`, call.index, call.itemID, call.callID, call.name, call.arguments))
	}
	const patch = "*** Begin Patch\n*** Update File: note.txt\n@@\n-old\n+verified\n*** End Patch\n"
	reduceOpenAIEvent(t, reducer, `{"type":"response.output_item.added","output_index":3,"item":{"type":"custom_tool_call","id":"ct-3","call_id":"call-3","name":"apply_patch"}}`)
	reduceOpenAIEvent(t, reducer, fmt.Sprintf(`{"type":"response.custom_tool_call_input.delta","item_id":"ct-3","output_index":3,"delta":%q}`, patch))
	reduceOpenAIEvent(t, reducer, fmt.Sprintf(`{"type":"response.custom_tool_call_input.done","item_id":"ct-3","output_index":3,"input":%q}`, patch))
	if len(reducer.readyCalls()) != 3 {
		t.Fatal("freeform input became ready before the native item completed")
	}
	reduceOpenAIEvent(t, reducer, fmt.Sprintf(`{"type":"response.output_item.done","output_index":3,"item":{"type":"custom_tool_call","id":"ct-3","call_id":"call-3","name":"apply_patch","input":%q}}`, patch))
	reduceOpenAIEvent(t, reducer, `{"type":"response.completed","response":{"id":"resp-tools"}}`)
	ready := reducer.readyCalls()
	if len(ready) != 4 || ready[0].Capability() != tool.CapabilityGlob || ready[1].Capability() != tool.CapabilityGrep || ready[2].Capability() != tool.CapabilityRead ||
		ready[3].Capability() != tool.CapabilityPatch || ready[3].WireKind() != tool.WireCustomFreeform || ready[3].ProviderCallID() != "call-3" {
		t.Fatalf("异构 ready calls = %#v", ready)
	}
	items := reducer.outputItems()
	if len(items) != 4 || items[3].Type != "custom_tool_call" || items[3].Input != patch || items[3].Arguments != "" {
		t.Fatalf("native items = %#v", items)
	}
}

func TestResponsesStreamReducerRejectsIncompleteReadArguments(t *testing.T) {
	reducer := newResponsesStreamReducer(testOpenAIToolCatalog(t))
	reduceOpenAIEvent(t, reducer, `{"type":"response.created","response":{"id":"resp-tool"}}`)
	reduceOpenAIEvent(t, reducer, `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc-1","call_id":"call-1","name":"Read"}}`)
	_, err := reducer.reduce(transport.SSEEvent{Data: `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc-1","call_id":"call-1","name":"Read","arguments":"{}"}}`})
	var faultError *fault.Error
	if !errors.As(err, &faultError) || faultError.Code != fault.CodeStreamProtocol || len(reducer.readyCalls()) != 0 {
		t.Fatalf("incomplete arguments error/state = %v/%#v", err, reducer.readyCalls())
	}
}

func assertOpenAIMetric(t *testing.T, metric domain.UsageMetric, state domain.UsageMetricState, value uint64, hasValue bool) {
	t.Helper()
	got, ok := metric.Value()
	if metric.State() != state || got != value || ok != hasValue {
		t.Fatalf("metric = state %q value %d ok %t, want state %q value %d ok %t", metric.State(), got, ok, state, value, hasValue)
	}
}

func reduceOpenAIEvent(t *testing.T, reducer *responsesStreamReducer, data string) reducerResult {
	t.Helper()
	result, err := reducer.reduce(transport.SSEEvent{Data: data})
	if err != nil {
		t.Fatalf("reduce event %s: %v", data, err)
	}
	return result
}

func TestResponsesRejectsUnfinishedOrDuplicatedMessage(t *testing.T) {
	const messageDone = `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg","role":"assistant","content":[{"type":"output_text","text":"answer"}]}}`
	for _, scenario := range []struct {
		name   string
		first  string
		second string
		reject string
	}{
		{
			name:   "unfinished message beside a completed reasoning item",
			first:  `{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg","role":"assistant","content":[]}}`,
			second: `{"type":"response.output_item.done","output_index":1,"item":{"type":"reasoning","id":"reason","encrypted_content":"opaque"}}`,
			reject: `{"type":"response.completed","response":{"id":"resp"}}`,
		},
		{
			name:   "duplicate completion with a conflicting snapshot",
			first:  messageDone,
			second: `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"another item"}]}}`,
			reject: `{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg","role":"assistant","content":[{"type":"output_text","text":"conflict"}]}}`,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			reducer := newResponsesStreamReducer()
			reduceOpenAIEvent(t, reducer, `{"type":"response.created","response":{"id":"resp"}}`)
			reduceOpenAIEvent(t, reducer, scenario.first)
			reduceOpenAIEvent(t, reducer, scenario.second)
			_, err := reducer.reduce(transport.SSEEvent{Data: scenario.reject})
			var failure *fault.Error
			if !errors.As(err, &failure) || failure.Code != fault.CodeStreamProtocol || reducer.terminal {
				t.Fatalf("invalid message stream resolved as terminal=%v, %v", reducer.terminal, err)
			}
		})
	}
}

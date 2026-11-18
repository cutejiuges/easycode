package anthropic

import (
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
)

func TestStreamReducerPreservesThinkingSignatureAndRedactedData(t *testing.T) {
	reducer := newStreamReducer()
	events := []string{
		`{"type":"message_start","message":{"id":"msg-1","model":"claude-test"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"duplicate","signature":"old"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque-signature"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"opaque-data","future":true}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_stop"}`,
	}
	semanticCount := 0
	for _, data := range events {
		result, err := reduceAnthropicTestEvent(reducer, data)
		if err != nil {
			t.Fatalf("reduce %s: %v", data, err)
		}
		if result.semantic != nil {
			semanticCount++
		}
	}
	blocks := reducer.assistantMessage().Content
	if semanticCount != 0 || len(blocks) != 2 {
		t.Fatalf("semantic=%d blocks=%#v", semanticCount, blocks)
	}
	if blocks[0].Thinking != "private" || blocks[0].Signature != "opaque-signature" {
		t.Fatalf("thinking block: %#v", blocks[0])
	}
	if blocks[1].RedactedData != "opaque-data" || len(blocks[1].Raw) == 0 {
		t.Fatalf("redacted block: %#v", blocks[1])
	}
}

func TestStreamReducerProducesOrderedHeterogeneousReadyCalls(t *testing.T) {
	reducer := newStreamReducer(testAnthropicToolCatalog(t))
	events := []string{
		`{"type":"message_start","message":{"id":"msg-tools","model":"claude-test"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu-0","name":"Glob","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"pattern\":\"**/*.go\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu-1","name":"Grep","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"pattern\":\"TODO\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu-2","name":"Read","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"README.md\"}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		if _, err := reduceAnthropicTestEvent(reducer, event); err != nil {
			t.Fatalf("reduce %s: %v", event, err)
		}
	}
	ready := reducer.readyCalls()
	if len(ready) != 3 || ready[0].Capability() != tool.CapabilityGlob || ready[1].Capability() != tool.CapabilityGrep || ready[2].Capability() != tool.CapabilityRead {
		t.Fatalf("异构 ready calls = %#v", ready)
	}
}

func TestStreamReducerRejectsIncompleteToolInput(t *testing.T) {
	reducer := newStreamReducer(testAnthropicToolCatalog(t))
	for _, event := range []string{
		`{"type":"message_start","message":{"id":"msg-tool","model":"claude-test"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu-1","name":"Read","input":{}}}`,
	} {
		if _, err := reduceAnthropicTestEvent(reducer, event); err != nil {
			t.Fatal(err)
		}
	}
	_, err := reduceAnthropicTestEvent(reducer, `{"type":"content_block_stop","index":0}`)
	assertFaultCode(t, err, fault.CodeStreamProtocol)
	if len(reducer.readyCalls()) != 0 {
		t.Fatalf("incomplete tool input produced ready calls: %#v", reducer.readyCalls())
	}
}

func TestStreamReducerProducesNormalizedUsage(t *testing.T) {
	reducer := newStreamReducer()
	for _, data := range []string{
		`{"type":"message_start","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":0,"cache_creation_input_tokens":3,"cache_read_input_tokens":7}}}`,
		textBlockStartEvent(0),
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`,
		`{"type":"message_stop"}`,
	} {
		if _, err := reduceAnthropicTestEvent(reducer, data); err != nil {
			t.Fatalf("reduce %s: %v", data, err)
		}
	}
	usage, err := reducer.sampleUsage()
	if err != nil {
		t.Fatal(err)
	}
	assertAnthropicMetric(t, usage.InputUncached(), domain.UsageMetricKnown, 0, true)
	assertAnthropicMetric(t, usage.CacheRead(), domain.UsageMetricKnown, 7, true)
	assertAnthropicMetric(t, usage.CacheWrite(), domain.UsageMetricKnown, 3, true)
	assertAnthropicMetric(t, usage.Output(), domain.UsageMetricKnown, 0, true)
	assertAnthropicMetric(t, usage.ReasoningOutput(), domain.UsageMetricUnknown, 0, false)
}

func assertAnthropicMetric(t *testing.T, metric domain.UsageMetric, state domain.UsageMetricState, value uint64, hasValue bool) {
	t.Helper()
	got, ok := metric.Value()
	if metric.State() != state || got != value || ok != hasValue {
		t.Fatalf("metric = state %q value %d ok %t, want state %q value %d ok %t", metric.State(), got, ok, state, value, hasValue)
	}
}

func reduceAnthropicTestEvent(reducer *streamReducer, data string) (reducerResult, error) {
	return reducer.reduce(transport.SSEEvent{Data: data})
}

func assertFaultCode(t *testing.T, err error, code fault.Code) {
	t.Helper()
	var faultError *fault.Error
	if !errors.As(err, &faultError) || faultError.Code != code {
		t.Fatalf("error: got %v want code %s", err, code)
	}
}

func textBlockStartEvent(index int) string {
	if index == 0 {
		return `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
	}
	return `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`
}

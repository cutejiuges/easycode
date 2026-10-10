package anthropic

import (
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
)

func TestStreamReducerBuildsOrderedTextWithoutDuplicatingStart(t *testing.T) {
	reducer := newStreamReducer()
	events := []string{
		`{"type":"message_start","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":4}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_stop"}`,
	}
	var text string
	var native *NativeItem
	completed := false
	for _, data := range events {
		result, err := reduceAnthropicTestEvent(reducer, data)
		if err != nil {
			t.Fatalf("reduce %s: %v", data, err)
		}
		if result.semantic != nil {
			payload, decodeErr := protocol.DecodeAssistantTextDelta(*result.semantic)
			if decodeErr != nil {
				t.Fatalf("decode text delta: %v", decodeErr)
			}
			text += payload.Text
		}
		if result.native != nil {
			native = result.native
		}
		completed = completed || result.complete
	}
	if text != "hello" || native == nil || native.Text != "hello" || !completed {
		t.Fatalf("reduced text=%q native=%#v completed=%t", text, native, completed)
	}
	assistant := reducer.assistantMessage()
	if len(assistant.Content) != 1 || assistant.Content[0].Text != "hello" {
		t.Fatalf("assistant message: %#v", assistant)
	}
}

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

func TestStreamReducerProducesReadyReadCall(t *testing.T) {
	reducer := newStreamReducer(testAnthropicToolCatalog(t))
	events := []string{
		`{"type":"message_start","message":{"id":"msg-tool","model":"claude-test"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu-1","name":"Read","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"README.md\",\"offset\":2,\"limit\":3}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_stop"}`,
	}
	for _, event := range events {
		if _, err := reduceAnthropicTestEvent(reducer, event); err != nil {
			t.Fatalf("reduce %s: %v", event, err)
		}
	}
	ready := reducer.readyCalls()
	assistant := reducer.assistantMessage()
	if len(ready) != 1 || ready[0].ProviderCallID() != tool.ProviderCallID("toolu-1") ||
		ready[0].ReadInput().FilePath() != "README.md" || ready[0].ReadInput().Offset() != 2 || ready[0].ReadInput().Limit() != 3 {
		t.Fatalf("ready calls = %#v", ready)
	}
	if len(assistant.Content) != 1 || string(assistant.Content[0].Input) != `{"file_path":"README.md","offset":2,"limit":3}` {
		t.Fatalf("assistant tool use = %#v", assistant)
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

func TestStreamReducerMergesCumulativeUsageWithoutInventingMissingValues(t *testing.T) {
	reducer := newStreamReducer()
	for _, data := range []string{
		`{"type":"message_start","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":12,"cache_creation_input_tokens":3,"cache_read_input_tokens":7}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":9}}`,
	} {
		if _, err := reduceAnthropicTestEvent(reducer, data); err != nil {
			t.Fatalf("reduce %s: %v", data, err)
		}
	}
	metadata := reducer.messageMetadata()
	if !metadata.StopReason.Known || metadata.StopReason.Value != "end_turn" {
		t.Fatalf("stop reason: %#v", metadata.StopReason)
	}
	usage := metadata.Usage
	if usage.InputTokens.Value != 12 || usage.CacheCreationInputTokens.Value != 3 || usage.CacheReadInputTokens.Value != 7 || usage.OutputTokens.Value != 9 {
		t.Fatalf("merged usage: %#v", usage)
	}
	if !usage.InputTokens.Known || !usage.CacheCreationInputTokens.Known || !usage.CacheReadInputTokens.Known || !usage.OutputTokens.Known {
		t.Fatalf("known usage lost: %#v", usage)
	}

	unknown := newStreamReducer()
	if _, err := reduceAnthropicTestEvent(unknown, `{"type":"message_start","message":{"id":"msg-2","model":"claude-test"}}`); err != nil {
		t.Fatalf("reduce missing usage: %v", err)
	}
	if unknown.messageMetadata().Usage.InputTokens.Known {
		t.Fatalf("missing usage became known: %#v", unknown.messageMetadata().Usage)
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
	assertAnthropicMetric(t, usage.ReasoningOutput(), domain.UsageMetricNotApplicable, 0, false)
}

func TestStreamReducerRejectsInvalidUsageNumbers(t *testing.T) {
	for _, test := range []struct {
		name   string
		events []string
	}{
		{name: "negative start", events: []string{`{"type":"message_start","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":-1}}}`}},
		{name: "overflow start", events: []string{`{"type":"message_start","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":18446744073709551616}}}`}},
		{name: "negative delta", events: []string{messageStartEvent(), `{"type":"message_delta","usage":{"output_tokens":-1}}`}},
		{name: "overflow delta", events: []string{messageStartEvent(), `{"type":"message_delta","usage":{"output_tokens":18446744073709551616}}`}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reducer := newStreamReducer()
			var err error
			for _, event := range test.events {
				_, err = reduceAnthropicTestEvent(reducer, event)
				if err != nil {
					break
				}
			}
			assertFaultCode(t, err, fault.CodeStreamProtocol)
		})
	}
}

func assertAnthropicMetric(t *testing.T, metric domain.UsageMetric, state domain.UsageMetricState, value uint64, hasValue bool) {
	t.Helper()
	got, ok := metric.Value()
	if metric.State() != state || got != value || ok != hasValue {
		t.Fatalf("metric = state %q value %d ok %t, want state %q value %d ok %t", metric.State(), got, ok, state, value, hasValue)
	}
}

func TestStreamReducerHandlesPingUnknownAndProviderError(t *testing.T) {
	reducer := newStreamReducer()
	for _, data := range []string{`{"type":"ping"}`, `{"type":"future_event","payload":{"enabled":true}}`} {
		result, err := reduceAnthropicTestEvent(reducer, data)
		if err != nil || result.semantic != nil || result.native != nil || result.complete {
			t.Fatalf("ignored event %s produced %#v error=%v", data, result, err)
		}
	}
	_, err := reduceAnthropicTestEvent(reducer, `{"type":"error","error":{"type":"authentication_error","message":"sensitive body"}}`)
	assertFaultCode(t, err, fault.CodeProviderRequest)
	if err != nil && err.Error() != "provider_request_failed: provider response failed" {
		t.Fatalf("provider error exposed response body: %v", err)
	}
}

func TestStreamReducerRejectsInvalidStateTransitions(t *testing.T) {
	tests := []struct {
		name   string
		events []string
	}{
		{name: "invalid JSON", events: []string{"{"}},
		{name: "missing type", events: []string{`{"index":0}`}},
		{name: "duplicate message start", events: []string{messageStartEvent(), messageStartEvent()}},
		{name: "block before message", events: []string{textBlockStartEvent(0)}},
		{name: "duplicate index", events: []string{messageStartEvent(), textBlockStartEvent(0), textBlockStartEvent(0)}},
		{name: "delta before start", events: []string{messageStartEvent(), textDeltaEvent(0, "x")}},
		{name: "stop before start", events: []string{messageStartEvent(), `{"type":"content_block_stop","index":0}`}},
		{name: "mismatched delta", events: []string{messageStartEvent(), textBlockStartEvent(0), `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"x"}}`}},
		{name: "active block at message stop", events: []string{messageStartEvent(), textBlockStartEvent(0), `{"type":"message_stop"}`}},
		{name: "message stop before start", events: []string{`{"type":"message_stop"}`}},
		{name: "message stop without blocks", events: []string{messageStartEvent(), `{"type":"message_stop"}`}},
		{name: "repeated block stop", events: []string{messageStartEvent(), textBlockStartEvent(0), `{"type":"content_block_stop","index":0}`, `{"type":"content_block_stop","index":0}`}},
		{name: "event after message stop", events: []string{messageStartEvent(), textBlockStartEvent(0), `{"type":"content_block_stop","index":0}`, `{"type":"message_stop"}`, `{"type":"ping"}`}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reducer := newStreamReducer()
			var err error
			for _, data := range test.events {
				_, err = reduceAnthropicTestEvent(reducer, data)
				if err != nil {
					break
				}
			}
			assertFaultCode(t, err, fault.CodeStreamProtocol)
		})
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

func messageStartEvent() string {
	return `{"type":"message_start","message":{"id":"msg-1","model":"claude-test"}}`
}

func textBlockStartEvent(index int) string {
	if index == 0 {
		return `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
	}
	return `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`
}

func textDeltaEvent(index int, text string) string {
	if index == 0 && text == "x" {
		return `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`
	}
	return `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"x"}}`
}

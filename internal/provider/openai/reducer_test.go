package openai

import (
	"errors"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
)

func TestResponsesStreamReducerAcceptsOrderedSample(t *testing.T) {
	reducer := newResponsesStreamReducer()
	result := reduceOpenAIEvent(t, reducer, `{"type":"response.created","response":{"id":"resp-1"}}`)
	if result.completed || reducer.responseIdentity() != "resp-1" {
		t.Fatalf("created result/state = %#v/%q", result, reducer.responseIdentity())
	}
	result = reduceOpenAIEvent(t, reducer, `{"type":"response.future.delta","delta":"ignored"}`)
	if result != (reducerResult{}) {
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

func TestResponsesStreamReducerPreservesMissingAndExplicitZeroUsage(t *testing.T) {
	missing := newResponsesStreamReducer()
	reduceOpenAIEvent(t, missing, `{"type":"response.created","response":{"id":"resp-1"}}`)
	reduceOpenAIEvent(t, missing, `{"type":"response.completed","response":{"id":"resp-1"}}`)
	missingUsage, err := missing.sampleUsage()
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []domain.UsageMetric{
		missingUsage.InputUncached(), missingUsage.CacheRead(), missingUsage.CacheWrite(),
		missingUsage.Output(), missingUsage.ReasoningOutput(),
	} {
		assertOpenAIMetric(t, metric, domain.UsageMetricUnknown, 0, false)
	}

	zero := newResponsesStreamReducer()
	reduceOpenAIEvent(t, zero, `{"type":"response.created","response":{"id":"resp-2"}}`)
	reduceOpenAIEvent(t, zero, `{"type":"response.completed","response":{"id":"resp-2","usage":{"input_tokens":0,"input_tokens_details":{"cached_tokens":0},"cache_write_tokens":0,"output_tokens":0,"output_tokens_details":{"reasoning_tokens":0}}}}`)
	zeroUsage, err := zero.sampleUsage()
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []domain.UsageMetric{
		zeroUsage.InputUncached(), zeroUsage.CacheRead(), zeroUsage.CacheWrite(),
		zeroUsage.Output(), zeroUsage.ReasoningOutput(),
	} {
		assertOpenAIMetric(t, metric, domain.UsageMetricKnown, 0, true)
	}
}

func TestResponsesStreamReducerRejectsInvalidUsage(t *testing.T) {
	for _, test := range []struct {
		name  string
		usage string
	}{
		{name: "negative", usage: `{"input_tokens":-1}`},
		{name: "overflow", usage: `{"input_tokens":18446744073709551616}`},
		{name: "cached above input", usage: `{"input_tokens":2,"input_tokens_details":{"cached_tokens":3}}`},
		{name: "reasoning above output", usage: `{"output_tokens":2,"output_tokens_details":{"reasoning_tokens":3}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			reducer := newResponsesStreamReducer()
			reduceOpenAIEvent(t, reducer, `{"type":"response.created","response":{"id":"resp-1"}}`)
			_, err := reducer.reduce(transport.SSEEvent{Data: `{"type":"response.completed","response":{"id":"resp-1","usage":` + test.usage + `}}`})
			var faultError *fault.Error
			if !errors.As(err, &faultError) || faultError.Code != fault.CodeStreamProtocol || reducer.terminal {
				t.Fatalf("error/state = %v/%#v", err, reducer)
			}
		})
	}
}

func assertOpenAIMetric(t *testing.T, metric domain.UsageMetric, state domain.UsageMetricState, value uint64, hasValue bool) {
	t.Helper()
	got, ok := metric.Value()
	if metric.State() != state || got != value || ok != hasValue {
		t.Fatalf("metric = state %q value %d ok %t, want state %q value %d ok %t", metric.State(), got, ok, state, value, hasValue)
	}
}

func TestResponsesStreamReducerRejectsIllegalStateTransitions(t *testing.T) {
	tests := []struct {
		name   string
		events []string
		code   fault.Code
	}{
		{name: "missing created", events: []string{`{"type":"response.output_text.delta","delta":"hello"}`}, code: fault.CodeStreamProtocol},
		{name: "duplicate created", events: []string{
			`{"type":"response.created","response":{"id":"resp-1"}}`,
			`{"type":"response.created","response":{"id":"resp-1"}}`,
		}, code: fault.CodeStreamProtocol},
		{name: "conflicting created", events: []string{
			`{"type":"response.created","response":{"id":"resp-1"}}`,
			`{"type":"response.created","response":{"id":"resp-2"}}`,
		}, code: fault.CodeStreamProtocol},
		{name: "completed identity conflict", events: []string{
			`{"type":"response.created","response":{"id":"resp-1"}}`,
			`{"type":"response.completed","response":{"id":"resp-2"}}`,
		}, code: fault.CodeStreamProtocol},
		{name: "failed identity conflict", events: []string{
			`{"type":"response.created","response":{"id":"resp-1"}}`,
			`{"type":"response.failed","response":{"id":"resp-2"}}`,
		}, code: fault.CodeStreamProtocol},
		{name: "incomplete identity conflict", events: []string{
			`{"type":"response.created","response":{"id":"resp-1"}}`,
			`{"type":"response.incomplete","response":{"id":"resp-2"}}`,
		}, code: fault.CodeStreamProtocol},
		{name: "duplicate terminal", events: []string{
			`{"type":"response.created","response":{"id":"resp-1"}}`,
			`{"type":"response.completed","response":{"id":"resp-1"}}`,
			`{"type":"response.completed","response":{"id":"resp-1"}}`,
		}, code: fault.CodeStreamProtocol},
		{name: "event after terminal", events: []string{
			`{"type":"response.created","response":{"id":"resp-1"}}`,
			`{"type":"response.completed","response":{"id":"resp-1"}}`,
			`{"type":"response.future.delta"}`,
		}, code: fault.CodeStreamProtocol},
		{name: "unknown before created", events: []string{`{"type":"response.future.delta"}`}, code: fault.CodeStreamProtocol},
		{name: "missing delta", events: []string{
			`{"type":"response.created","response":{"id":"resp-1"}}`,
			`{"type":"response.output_text.delta"}`,
		}, code: fault.CodeStreamProtocol},
		{name: "invalid JSON", events: []string{`{`}, code: fault.CodeStreamProtocol},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reducer := newResponsesStreamReducer()
			var err error
			for _, data := range test.events {
				_, err = reducer.reduce(transport.SSEEvent{Data: data})
				if err != nil {
					break
				}
			}
			var faultError *fault.Error
			if !errors.As(err, &faultError) || faultError.Code != test.code {
				t.Fatalf("error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestResponsesStreamReducerValidatesFailedAndIncompleteIdentity(t *testing.T) {
	for _, test := range []struct {
		name     string
		terminal string
	}{
		{name: "failed", terminal: `{"type":"response.failed","response":{"id":"resp-1","error":{"code":"rate_limit_exceeded"}}}`},
		{name: "incomplete", terminal: `{"type":"response.incomplete","response":{"id":"resp-1"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			reducer := newResponsesStreamReducer()
			reduceOpenAIEvent(t, reducer, `{"type":"response.created","response":{"id":"resp-1"}}`)
			_, err := reducer.reduce(transport.SSEEvent{Data: test.terminal})
			var faultError *fault.Error
			if !errors.As(err, &faultError) || faultError.Code != fault.CodeProviderRequest || !reducer.terminal {
				t.Fatalf("terminal error/state = %v/%#v", err, reducer)
			}
		})
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

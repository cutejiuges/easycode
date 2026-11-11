package openai

import (
	"testing"

	"easycode/internal/provider/transport"
)

func TestReasoningUsageDuplicateFieldsCannotBecomeSuccessfulSample(t *testing.T) {
	for _, response := range []string{
		`{"id":"usage-regression","usage":{"output_tokens":40,"output_tokens_details":{"reasoning_tokens":null},"output_tokens_details":{"reasoning_tokens":30}}}`,
		`{"id":"usage-regression","usage":{"output_tokens":40,"output_tokens_details":{"reasoning_tokens":30,"reasoning_tokens":0}}}`,
		`{"id":"usage-regression","usage":{"output_tokens":20,"output_tokens":40,"output_tokens_details":{"reasoning_tokens":30}}}`,
		`{"id":"wrong-response","id":"usage-regression","usage":{"output_tokens":40}}`,
		`{"id":"usage-regression","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":120},"input_tokens_details":{"cached_tokens":10}}}`,
	} {
		t.Run(response, func(t *testing.T) {
			r := newResponsesStreamReducer(testOpenAIToolCatalog(t))
			reduceOpenAIEvent(t, r, `{"type":"response.created","response":{"id":"usage-regression"}}`)
			result, err := r.reduce(transport.SSEEvent{Data: `{"type":"response.completed","response":` + response + `}`})
			if err == nil || result.completed || r.terminal {
				t.Fatal("ambiguous completed response became a trusted terminal")
			}
			if _, err := r.sampleUsage(); err == nil {
				t.Fatal("ambiguous response exposed sample usage")
			}
		})
	}
}

func TestReasoningUsageAcceptsWellFormedProviderExtensions(t *testing.T) {
	r := newResponsesStreamReducer()
	reduceOpenAIEvent(t, r, `{"type":"response.created","response":{"id":"usage-regression"},"future":{"valid":true}}`)
	result := reduceOpenAIEvent(t, r, `{"type":"response.completed","response":{"id":"usage-regression","usage":{"output_tokens":40,"output_tokens_details":{"reasoning_tokens":30,"future":3}},"future":{"valid":true}}}`)
	if !result.completed {
		t.Fatal("valid provider extension blocked completion")
	}
	usage, err := r.sampleUsage()
	if err != nil {
		t.Fatal(err)
	}
	if value, known := usage.ReasoningOutput().Value(); !known || value != 30 {
		t.Fatal("valid extension changed reasoning usage")
	}
}

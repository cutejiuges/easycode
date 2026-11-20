package anthropic

import (
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
)

func TestThinkingLifecycleWhitelistAndNativeSignature(t *testing.T) {
	r := newStreamReducer()
	frames := []string{
		`{"type":"message_start","message":{"id":"m","model":"fixture"}}`,
		`{"type":"content_block_start","index":4,"content_block":{"type":"thinking","thinking":"ignored-start","signature":"ignored-signature"}}`,
		`{"type":"content_block_delta","index":4,"delta":{"type":"thinking_delta","thinking":"  中文🙂\n"}}`,
		`{"type":"content_block_delta","index":4,"delta":{"type":"signature_delta","signature":"signature-sentinel"}}`,
		`{"type":"content_block_stop","index":4}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"redacted_thinking","data":"redacted-sentinel"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_stop"}`,
	}
	var state protocol.ReasoningStreamState
	var exposed strings.Builder
	stages := []protocol.ReasoningStage{}
	for _, frame := range frames {
		result, err := r.reduce(transport.SSEEvent{Data: frame})
		if err != nil {
			t.Fatal(err)
		}
		output := make(chan provider.StreamEvent, 16)
		if err := provider.PublishReasoning(result.reasoning, func(event provider.StreamEvent) error {
			output <- event
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		close(output)
		for event := range output {
			exposed.Write(event.Semantic().Payload)
			p, err := protocol.DecodeAssistantReasoning(event.Semantic())
			if err != nil {
				t.Fatal(err)
			}
			if err := state.Accept(p); err != nil {
				t.Fatal(err)
			}
			stages = append(stages, p.Stage)
		}
	}
	if len(stages) != 5 || state.Complete() != nil {
		t.Fatal(stages)
	}
	for _, secret := range []string{"ignored-start", "ignored-signature", "signature-sentinel", "redacted-sentinel"} {
		if strings.Contains(exposed.String(), secret) {
			t.Fatal("private source leaked", secret)
		}
	}
	native := r.assistantMessage()
	if native.Content[1].Thinking != "  中文🙂\n" || native.Content[1].Signature != "signature-sentinel" || native.Content[0].RedactedData != "redacted-sentinel" {
		t.Fatal("native fidelity lost")
	}
	presentation, err := r.presentation()
	if err != nil {
		t.Fatal(err)
	}
	bindings := presentation.Bindings()
	if len(bindings) != 2 || bindings[0].Item != 1 || bindings[1].Item != 0 || bindings[1].Availability != domain.ReasoningUnavailable {
		t.Fatal(bindings)
	}
}

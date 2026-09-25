package openai

import (
	"errors"
	"testing"

	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
)

func TestReduceResponsesEvents(t *testing.T) {
	tests := []struct {
		name          string
		data          string
		wantText      string
		wantNative    string
		wantCompleted bool
		wantResponse  string
		wantCode      fault.Code
	}{
		{name: "created", data: `{"type":"response.created","response":{"id":"resp-1"}}`, wantResponse: "resp-1"},
		{name: "text delta", data: `{"type":"response.output_text.delta","delta":"hello"}`, wantText: "hello"},
		{name: "output item", data: `{"type":"response.output_item.done","item":{"type":"message","id":"msg-1","role":"assistant","content":[{"type":"output_text","text":"hello"}],"future":true}}`, wantNative: "msg-1"},
		{name: "completed", data: `{"type":"response.completed","response":{"id":"resp-1"}}`, wantCompleted: true, wantResponse: "resp-1"},
		{name: "failed", data: `{"type":"response.failed","response":{"id":"resp-1","error":{"code":"rate_limit_exceeded"}}}`, wantCode: fault.CodeProviderRequest},
		{name: "incomplete", data: `{"type":"response.incomplete","response":{"id":"resp-1"}}`, wantCode: fault.CodeProviderRequest},
		{name: "unknown", data: `{"type":"response.future.delta","delta":"ignored"}`},
		{name: "missing delta", data: `{"type":"response.output_text.delta"}`, wantCode: fault.CodeStreamProtocol},
		{name: "invalid JSON", data: `{`, wantCode: fault.CodeStreamProtocol},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := reduceResponsesEvent(transport.SSEEvent{Data: test.data})
			if test.wantCode != "" {
				var faultError *fault.Error
				if !errors.As(err, &faultError) || faultError.Code != test.wantCode {
					t.Fatalf("error: got %v want code %s", err, test.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("reduce event: %v", err)
			}
			if test.wantText != "" {
				if result.semantic == nil {
					t.Fatal("missing semantic event")
				}
				payload, decodeErr := protocol.DecodeAssistantTextDelta(*result.semantic)
				if decodeErr != nil || payload.Text != test.wantText {
					t.Fatalf("text payload: %#v err=%v", payload, decodeErr)
				}
			}
			if test.wantNative != "" {
				if result.native == nil || result.native.ID != test.wantNative || len(result.native.Raw) == 0 {
					t.Fatalf("native result: %#v", result.native)
				}
			}
			if result.completed != test.wantCompleted || result.responseID != test.wantResponse {
				t.Fatalf("result: %#v", result)
			}
		})
	}
}

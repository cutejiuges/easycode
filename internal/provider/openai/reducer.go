package openai

import (
	"encoding/json"

	"easycode/internal/codec"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
)

type responsesEventEnvelope struct {
	Type     string          `json:"type"`
	Delta    *string         `json:"delta,omitempty"`
	Item     json.RawMessage `json:"item,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
}

type responseReference struct {
	ID string `json:"id"`
}

type reducerResult struct {
	semantic   *protocol.Event
	native     *NativeItem
	completed  bool
	responseID string
}

func reduceResponsesEvent(event transport.SSEEvent) (reducerResult, error) {
	var envelope responsesEventEnvelope
	if err := codec.Unmarshal([]byte(event.Data), &envelope); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses event is invalid JSON", err)
	}
	if envelope.Type == "" {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses event type is required")
	}

	switch envelope.Type {
	case "response.created":
		response, err := decodeResponseReference(envelope.Response)
		if err != nil {
			return reducerResult{}, err
		}
		return reducerResult{responseID: response.ID}, nil
	case "response.output_text.delta":
		if envelope.Delta == nil {
			return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses text delta is required")
		}
		if *envelope.Delta == "" {
			return reducerResult{}, nil
		}
		semantic, err := protocol.NewAssistantTextDelta(*envelope.Delta)
		if err != nil {
			return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses text delta is invalid", err)
		}
		return reducerResult{semantic: &semantic}, nil
	case "response.output_item.done":
		if len(envelope.Item) == 0 || string(envelope.Item) == "null" {
			return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses output item is required")
		}
		var item NativeItem
		if err := codec.Unmarshal(envelope.Item, &item); err != nil {
			return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses output item is invalid", err)
		}
		return reducerResult{native: &item}, nil
	case "response.completed":
		response, err := decodeResponseReference(envelope.Response)
		if err != nil {
			return reducerResult{}, err
		}
		return reducerResult{completed: true, responseID: response.ID}, nil
	case "response.failed":
		if len(envelope.Response) == 0 || string(envelope.Response) == "null" {
			return reducerResult{}, fault.New(fault.CodeProviderRequest, "provider response failed")
		}
		return reducerResult{}, fault.New(fault.CodeProviderRequest, "provider response failed")
	case "response.incomplete":
		return reducerResult{}, fault.New(fault.CodeProviderRequest, "provider response is incomplete")
	default:
		return reducerResult{}, nil
	}
}

func decodeResponseReference(raw json.RawMessage) (responseReference, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return responseReference{}, fault.New(fault.CodeStreamProtocol, "Responses response metadata is required")
	}
	var response responseReference
	if err := codec.Unmarshal(raw, &response); err != nil {
		return responseReference{}, fault.Wrap(fault.CodeStreamProtocol, "Responses response metadata is invalid", err)
	}
	if response.ID == "" {
		return responseReference{}, fault.New(fault.CodeStreamProtocol, "Responses response ID is required")
	}
	return response, nil
}

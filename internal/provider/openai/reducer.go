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
	semantic  *protocol.Event
	native    *NativeItem
	completed bool
}

type responsesStreamReducer struct {
	created    bool
	responseID string
	terminal   bool
	items      []NativeItem
}

func newResponsesStreamReducer() *responsesStreamReducer {
	return &responsesStreamReducer{items: make([]NativeItem, 0)}
}

func (reducer *responsesStreamReducer) reduce(event transport.SSEEvent) (reducerResult, error) {
	if reducer == nil {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses reducer is required")
	}
	if reducer.terminal {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses event arrived after terminal")
	}
	var envelope responsesEventEnvelope
	if err := codec.Unmarshal([]byte(event.Data), &envelope); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses event is invalid JSON", err)
	}
	if envelope.Type == "" {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses event type is required")
	}

	if envelope.Type == "response.created" {
		if reducer.created {
			return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses response.created is duplicated")
		}
		response, err := decodeResponseReference(envelope.Response)
		if err != nil {
			return reducerResult{}, err
		}
		reducer.created = true
		reducer.responseID = response.ID
		return reducerResult{}, nil
	}
	if !reducer.created {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses event arrived before response.created")
	}

	switch envelope.Type {
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
		item = item.clone()
		reducer.items = append(reducer.items, item)
		return reducerResult{native: &item}, nil
	case "response.completed":
		if err := reducer.acceptTerminal(envelope.Response); err != nil {
			return reducerResult{}, err
		}
		return reducerResult{completed: true}, nil
	case "response.failed":
		if err := reducer.acceptTerminal(envelope.Response); err != nil {
			return reducerResult{}, err
		}
		return reducerResult{}, fault.New(fault.CodeProviderRequest, "provider response failed")
	case "response.incomplete":
		if err := reducer.acceptTerminal(envelope.Response); err != nil {
			return reducerResult{}, err
		}
		return reducerResult{}, fault.New(fault.CodeProviderRequest, "provider response is incomplete")
	default:
		return reducerResult{}, nil
	}
}

func (reducer *responsesStreamReducer) acceptTerminal(raw json.RawMessage) error {
	response, err := decodeResponseReference(raw)
	if err != nil {
		return err
	}
	if response.ID != reducer.responseID {
		return fault.New(fault.CodeStreamProtocol, "Responses terminal response ID does not match response.created")
	}
	reducer.terminal = true
	return nil
}

func (reducer *responsesStreamReducer) responseIdentity() string {
	if reducer == nil {
		return ""
	}
	return reducer.responseID
}

func (reducer *responsesStreamReducer) outputItems() []NativeItem {
	if reducer == nil {
		return nil
	}
	return cloneNativeItems(reducer.items)
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

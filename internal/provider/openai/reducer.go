package openai

import (
	"encoding/json"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
)

const maxFunctionArgumentsBytes = 1 << 20

type responsesEventEnvelope struct {
	Type        string          `json:"type"`
	Delta       *string         `json:"delta,omitempty"`
	Arguments   *string         `json:"arguments,omitempty"`
	ItemID      string          `json:"item_id,omitempty"`
	OutputIndex *int            `json:"output_index,omitempty"`
	Item        json.RawMessage `json:"item,omitempty"`
	Response    json.RawMessage `json:"response,omitempty"`
}

type responseReference struct {
	ID string `json:"id"`
}

type responseCompletedWire struct {
	ID    string              `json:"id"`
	Usage *responsesUsageWire `json:"usage,omitempty"`
}

type responsesUsageWire struct {
	InputTokens         *uint64                  `json:"input_tokens,omitempty"`
	InputTokensDetails  *inputTokensDetailsWire  `json:"input_tokens_details,omitempty"`
	CacheWriteTokens    *uint64                  `json:"cache_write_tokens,omitempty"`
	OutputTokens        *uint64                  `json:"output_tokens,omitempty"`
	OutputTokensDetails *outputTokensDetailsWire `json:"output_tokens_details,omitempty"`
}

type inputTokensDetailsWire struct {
	CachedTokens *uint64 `json:"cached_tokens,omitempty"`
}

type outputTokensDetailsWire struct {
	ReasoningTokens *uint64 `json:"reasoning_tokens,omitempty"`
}

type reducerResult struct {
	semantic  *protocol.Event
	native    *NativeItem
	completed bool
}

type responsesStreamReducer struct {
	created                 bool
	responseID              string
	terminal                bool
	items                   []NativeItem
	functions               map[string]*functionCallState
	ready                   []tool.ReadyCall
	catalog                 tool.CatalogSnapshot
	usage                   rawUsage
	lastFunctionOutputIndex int
}

type functionCallState struct {
	itemID        string
	callID        string
	name          string
	arguments     string
	outputIndex   int
	argumentsDone bool
	itemDone      bool
}

func newResponsesStreamReducer(catalog ...tool.CatalogSnapshot) *responsesStreamReducer {
	reducer := &responsesStreamReducer{
		items: make([]NativeItem, 0), functions: make(map[string]*functionCallState),
		ready: make([]tool.ReadyCall, 0), lastFunctionOutputIndex: -1,
	}
	if len(catalog) == 1 {
		reducer.catalog = catalog[0].Clone()
	}
	return reducer
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
	case "response.output_item.added":
		return reducer.reduceOutputItemAdded(envelope.OutputIndex, envelope.Item)
	case "response.function_call_arguments.delta":
		return reducer.reduceFunctionArgumentsDelta(envelope.ItemID, envelope.OutputIndex, envelope.Delta)
	case "response.function_call_arguments.done":
		return reducer.reduceFunctionArgumentsDone(envelope.ItemID, envelope.OutputIndex, envelope.Arguments)
	case "response.output_item.done":
		if len(envelope.Item) == 0 || string(envelope.Item) == "null" {
			return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses output item is required")
		}
		var item NativeItem
		if err := codec.Unmarshal(envelope.Item, &item); err != nil {
			return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses output item is invalid", err)
		}
		item = item.clone()
		if item.Type == "function_call" {
			ready, err := reducer.completeFunctionCall(envelope.OutputIndex, item)
			if err != nil {
				return reducerResult{}, err
			}
			item.Arguments = reducer.functions[item.ID].arguments
			reducer.ready = append(reducer.ready, ready)
		}
		reducer.items = append(reducer.items, item)
		return reducerResult{native: &item}, nil
	case "response.completed":
		if err := reducer.acceptCompleted(envelope.Response); err != nil {
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

func (reducer *responsesStreamReducer) acceptCompleted(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return fault.New(fault.CodeStreamProtocol, "Responses response metadata is required")
	}
	var response responseCompletedWire
	if err := codec.Unmarshal(raw, &response); err != nil {
		return fault.Wrap(fault.CodeStreamProtocol, "Responses response metadata is invalid", err)
	}
	if response.ID == "" || response.ID != reducer.responseID {
		return fault.New(fault.CodeStreamProtocol, "Responses terminal response ID does not match response.created")
	}
	for _, state := range reducer.functions {
		if !state.argumentsDone || !state.itemDone {
			return fault.New(fault.CodeStreamProtocol, "Responses function call is incomplete")
		}
	}
	usage := decodeResponsesUsage(response.Usage)
	if _, err := usage.normalized(); err != nil {
		return fault.Wrap(fault.CodeStreamProtocol, "Responses usage is invalid", err)
	}
	reducer.usage = usage
	reducer.terminal = true
	return nil
}

func (reducer *responsesStreamReducer) reduceOutputItemAdded(index *int, raw json.RawMessage) (reducerResult, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses output item is required")
	}
	var item NativeItem
	if err := codec.Unmarshal(raw, &item); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses output item is invalid", err)
	}
	if item.Type != "function_call" {
		return reducerResult{}, nil
	}
	if index == nil || *index < 0 || item.ID == "" || item.CallID == "" || item.Name == "" {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses function call identity is invalid")
	}
	if _, exists := reducer.functions[item.ID]; exists {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses function call item is duplicated")
	}
	for _, state := range reducer.functions {
		if state.callID == item.CallID || state.outputIndex == *index {
			return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses function call identity conflicts")
		}
	}
	reducer.functions[item.ID] = &functionCallState{
		itemID: item.ID, callID: item.CallID, name: item.Name, arguments: item.Arguments, outputIndex: *index,
	}
	return reducerResult{}, nil
}

func (reducer *responsesStreamReducer) reduceFunctionArgumentsDelta(itemID string, index *int, delta *string) (reducerResult, error) {
	state, err := reducer.functionState(itemID, index)
	if err != nil {
		return reducerResult{}, err
	}
	if delta == nil || state.argumentsDone || len(state.arguments)+len(*delta) > maxFunctionArgumentsBytes {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses function arguments delta is invalid")
	}
	state.arguments += *delta
	return reducerResult{}, nil
}

func (reducer *responsesStreamReducer) reduceFunctionArgumentsDone(itemID string, index *int, arguments *string) (reducerResult, error) {
	state, err := reducer.functionState(itemID, index)
	if err != nil {
		return reducerResult{}, err
	}
	if arguments == nil || state.argumentsDone || len(*arguments) > maxFunctionArgumentsBytes {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses completed function arguments are invalid")
	}
	if state.arguments != "" && state.arguments != *arguments {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses function arguments conflict")
	}
	state.arguments = *arguments
	state.argumentsDone = true
	return reducerResult{}, nil
}

func (reducer *responsesStreamReducer) completeFunctionCall(index *int, item NativeItem) (tool.ReadyCall, error) {
	state, err := reducer.functionState(item.ID, index)
	if err != nil {
		return tool.ReadyCall{}, err
	}
	if state.itemDone || !state.argumentsDone || item.CallID != state.callID || item.Name != state.name ||
		item.Arguments != state.arguments || state.outputIndex <= reducer.lastFunctionOutputIndex {
		return tool.ReadyCall{}, fault.New(fault.CodeStreamProtocol, "Responses completed function call is invalid")
	}
	input, err := reducer.catalog.DecodeRead(domain.ProviderOpenAI, state.name, []byte(state.arguments))
	if err != nil {
		return tool.ReadyCall{}, fault.New(fault.CodeStreamProtocol, "Responses function arguments are invalid")
	}
	callID, err := tool.ParseProviderCallID(state.callID)
	if err != nil {
		return tool.ReadyCall{}, fault.New(fault.CodeStreamProtocol, "Responses function call ID is invalid")
	}
	ready, err := tool.NewReadyCall(callID, input)
	if err != nil {
		return tool.ReadyCall{}, fault.Wrap(fault.CodeStreamProtocol, "Responses ready call is invalid", err)
	}
	state.itemDone = true
	reducer.lastFunctionOutputIndex = state.outputIndex
	return ready, nil
}

func (reducer *responsesStreamReducer) functionState(itemID string, index *int) (*functionCallState, error) {
	if itemID == "" || index == nil || *index < 0 {
		return nil, fault.New(fault.CodeStreamProtocol, "Responses function call reference is invalid")
	}
	state, exists := reducer.functions[itemID]
	if !exists || state.outputIndex != *index {
		return nil, fault.New(fault.CodeStreamProtocol, "Responses function call reference is unknown")
	}
	return state, nil
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

func (reducer *responsesStreamReducer) readyCalls() []tool.ReadyCall {
	if reducer == nil {
		return nil
	}
	ready := make([]tool.ReadyCall, len(reducer.ready))
	for index, call := range reducer.ready {
		ready[index] = call.Clone()
	}
	return ready
}

func (reducer *responsesStreamReducer) rawUsage() rawUsage {
	if reducer == nil {
		return rawUsage{}
	}
	return reducer.usage.clone()
}

func (reducer *responsesStreamReducer) sampleUsage() (domain.SampleUsage, error) {
	if reducer == nil || !reducer.terminal {
		return domain.SampleUsage{}, fault.New(fault.CodeStreamProtocol, "Responses sample usage is unavailable")
	}
	return reducer.usage.normalized()
}

func decodeResponsesUsage(wire *responsesUsageWire) rawUsage {
	if wire == nil {
		return rawUsage{}
	}
	usage := rawUsage{
		InputTokens:      optionalUintFromPointer(wire.InputTokens),
		CacheWriteTokens: optionalUintFromPointer(wire.CacheWriteTokens),
		OutputTokens:     optionalUintFromPointer(wire.OutputTokens),
	}
	if wire.InputTokensDetails != nil {
		usage.CachedInputTokens = optionalUintFromPointer(wire.InputTokensDetails.CachedTokens)
	}
	if wire.OutputTokensDetails != nil {
		usage.ReasoningOutputTokens = optionalUintFromPointer(wire.OutputTokensDetails.ReasoningTokens)
	}
	return usage
}

func optionalUintFromPointer(value *uint64) optionalUint {
	if value == nil {
		return optionalUint{}
	}
	return optionalUint{Known: true, Value: *value}
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

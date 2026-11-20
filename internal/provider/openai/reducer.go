package openai

import (
	"encoding/json"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
)

type responsesEventEnvelope struct {
	Type         string          `json:"type"`
	Delta        *string         `json:"delta,omitempty"`
	Arguments    *string         `json:"arguments,omitempty"`
	Input        *string         `json:"input,omitempty"`
	ItemID       string          `json:"item_id,omitempty"`
	SummaryIndex *int            `json:"summary_index,omitempty"`
	Text         *string         `json:"text,omitempty"`
	Part         json.RawMessage `json:"part,omitempty"`
	OutputIndex  *int            `json:"output_index,omitempty"`
	Item         json.RawMessage `json:"item,omitempty"`
	Response     json.RawMessage `json:"response,omitempty"`
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
	reasoning provider.ReasoningDrafts
	native    *NativeItem
	completed bool
}

type responsesStreamReducer struct {
	reasoning           map[string]*reasoningItemState
	reasoningBudget     provider.ReasoningBudget
	reasoningCount      uint32
	bindings            []domain.ReasoningBinding
	created             bool
	responseID          string
	terminal            bool
	items               []NativeItem
	toolCalls           map[string]*toolCallState
	toolCallIDs         map[string]struct{}
	outputIDs           map[string]*outputIdentity
	outputIndexes       map[int]*outputIdentity
	pendingMessages     int
	pendingFunctions    int
	pendingCustoms      int
	ready               []tool.ReadyCall
	catalog             tool.CatalogSnapshot
	usage               rawUsage
	lastToolOutputIndex int
}

func newResponsesStreamReducer(catalog ...tool.CatalogSnapshot) *responsesStreamReducer {
	reducer := &responsesStreamReducer{
		reasoning: make(map[string]*reasoningItemState),
		bindings:  make([]domain.ReasoningBinding, 0), items: make([]NativeItem, 0),
		toolCalls: make(map[string]*toolCallState), toolCallIDs: make(map[string]struct{}),
		outputIDs: make(map[string]*outputIdentity), outputIndexes: make(map[int]*outputIdentity),
		ready: make([]tool.ReadyCall, 0), lastToolOutputIndex: -1,
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
	data := []byte(event.Data)
	// 在归并或确认 terminal 前校验完整帧，禁止重复成员覆盖计量与响应身份。
	if err := codec.ValidateJSON(data); err != nil {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses event is invalid JSON")
	}
	var envelope responsesEventEnvelope
	if err := codec.Unmarshal(data, &envelope); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses event is invalid JSON", err)
	}
	if envelope.Type == "" {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses event type is required")
	}
	if envelope.Type == "error" {
		reducer.terminal = true
		return reducerResult{}, fault.New(fault.CodeProviderRequest, "provider reported a Responses error")
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
	case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done":
		return reducer.reduceReasoningPart(envelope)
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
		return reducer.reduceToolInputDelta(tool.WireStructured, envelope.ItemID, envelope.OutputIndex, envelope.Delta)
	case "response.function_call_arguments.done":
		return reducer.reduceToolInputDone(tool.WireStructured, envelope.ItemID, envelope.OutputIndex, envelope.Arguments)
	case "response.custom_tool_call_input.delta":
		return reducer.reduceToolInputDelta(tool.WireCustomFreeform, envelope.ItemID, envelope.OutputIndex, envelope.Delta)
	case "response.custom_tool_call_input.done":
		return reducer.reduceToolInputDone(tool.WireCustomFreeform, envelope.ItemID, envelope.OutputIndex, envelope.Input)
	case "response.output_item.done":
		if len(envelope.Item) == 0 || string(envelope.Item) == "null" {
			return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses output item is required")
		}
		if err := reducer.preflightReasoningItem(envelope.Item); err != nil {
			return reducerResult{}, err
		}
		var item NativeItem
		if err := codec.Unmarshal(envelope.Item, &item); err != nil {
			return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses output item is invalid", err)
		}
		if err := reducer.observeOutputIdentity(item, envelope.OutputIndex, true); err != nil {
			return reducerResult{}, err
		}
		if item.Type == "reasoning" {
			return reducer.completeReasoning(envelope.OutputIndex, item)
		}
		if item.Type == "function_call" || item.Type == "custom_tool_call" {
			ready, err := reducer.completeToolCall(envelope.OutputIndex, item)
			if err != nil {
				return reducerResult{}, err
			}
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
	if reducer.pendingFunctions != 0 {
		return fault.New(fault.CodeStreamProtocol, "Responses function call is incomplete")
	}
	if reducer.pendingCustoms != 0 {
		return fault.New(fault.CodeStreamProtocol, "Responses custom tool call is incomplete")
	}
	for _, state := range reducer.reasoning {
		if !state.closed {
			return fault.New(fault.CodeStreamProtocol, "Responses reasoning item is incomplete")
		}
	}
	if reducer.pendingMessages != 0 {
		return fault.New(fault.CodeStreamProtocol, "Responses message item is incomplete")
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
	if err := reducer.preflightReasoningItem(raw); err != nil {
		return reducerResult{}, err
	}
	var item NativeItem
	if err := codec.Unmarshal(raw, &item); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Responses output item is invalid", err)
	}
	if err := reducer.observeOutputIdentity(item, index, false); err != nil {
		return reducerResult{}, err
	}
	if item.Type == "reasoning" {
		return reducer.startReasoning(index, item)
	}
	if item.Type != "function_call" && item.Type != "custom_tool_call" {
		return reducerResult{}, nil
	}
	return reducer.addToolCall(index, item)
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
	return reducer.usage
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

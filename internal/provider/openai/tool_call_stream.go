package openai

import (
	"strings"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/tool"
)

const maxFunctionArgumentsBytes = 1 << 20

// toolCallState 以同一模型归并原生工具调用身份、输入与完成边界。
type toolCallState struct {
	wire        tool.ProviderWireKind
	callID      string
	name        string
	input       strings.Builder
	outputIndex int
	inputDone   bool
	itemDone    bool
}

type toolCallFormat struct {
	callLabel  string
	inputLabel string
	doneError  string
	conflict   string
	readyLabel string
	wireLabel  string
	maxBytes   int
}

func streamedToolCallFormat(wire tool.ProviderWireKind) toolCallFormat {
	if wire == tool.WireCustomFreeform {
		return toolCallFormat{
			callLabel: "custom tool call", inputLabel: "custom tool input", conflict: "custom tool input conflicts",
			doneError:  "Responses completed custom tool input is invalid",
			readyLabel: "custom ready call", wireLabel: "custom", maxBytes: tool.MaxPatchInputBytes,
		}
	}
	return toolCallFormat{
		callLabel: "function call", inputLabel: "function arguments", conflict: "function arguments conflict",
		doneError:  "Responses completed function arguments are invalid",
		readyLabel: "ready call", wireLabel: "function", maxBytes: maxFunctionArgumentsBytes,
	}
}

func (reducer *responsesStreamReducer) addToolCall(index *int, item NativeItem) (reducerResult, error) {
	if index == nil || *index < 0 || item.ID == "" || item.CallID == "" || item.Name == "" {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses tool call identity is invalid")
	}
	if _, exists := reducer.toolCallIDs[item.CallID]; exists {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses tool call identity conflicts")
	}
	wire, input := nativeToolCallInput(item)
	state := &toolCallState{wire: wire, callID: item.CallID, name: item.Name, outputIndex: *index}
	state.input.WriteString(input)
	reducer.toolCalls[item.ID] = state
	reducer.toolCallIDs[item.CallID] = struct{}{}
	if wire == tool.WireStructured {
		reducer.pendingFunctions++
	} else {
		reducer.pendingCustoms++
	}
	return reducerResult{}, nil
}

func (reducer *responsesStreamReducer) toolCallState(wire tool.ProviderWireKind, format toolCallFormat, itemID string, index *int) (*toolCallState, error) {
	if itemID == "" || index == nil || *index < 0 {
		return nil, fault.New(fault.CodeStreamProtocol, "Responses "+format.callLabel+" reference is invalid")
	}
	state := reducer.toolCalls[itemID]
	if state == nil || state.wire != wire || state.outputIndex != *index {
		return nil, fault.New(fault.CodeStreamProtocol, "Responses "+format.callLabel+" reference is unknown")
	}
	return state, nil
}

func (reducer *responsesStreamReducer) reduceToolInputDelta(wire tool.ProviderWireKind, itemID string, index *int, delta *string) (reducerResult, error) {
	format := streamedToolCallFormat(wire)
	state, err := reducer.toolCallState(wire, format, itemID, index)
	if err != nil {
		return reducerResult{}, err
	}
	if delta == nil || state.inputDone || state.input.Len()+len(*delta) > format.maxBytes {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses "+format.inputLabel+" delta is invalid")
	}
	state.input.WriteString(*delta)
	return reducerResult{}, nil
}

func (reducer *responsesStreamReducer) reduceToolInputDone(wire tool.ProviderWireKind, itemID string, index *int, input *string) (reducerResult, error) {
	format := streamedToolCallFormat(wire)
	state, err := reducer.toolCallState(wire, format, itemID, index)
	if err != nil {
		return reducerResult{}, err
	}
	if input == nil || state.inputDone || len(*input) > format.maxBytes {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, format.doneError)
	}
	if state.input.Len() == 0 {
		state.input.WriteString(*input)
	} else if state.input.String() != *input {
		return reducerResult{}, fault.New(fault.CodeStreamProtocol, "Responses "+format.conflict)
	}
	state.inputDone = true
	return reducerResult{}, nil
}

func (reducer *responsesStreamReducer) completeToolCall(index *int, item NativeItem) (tool.ReadyCall, error) {
	wire, input := nativeToolCallInput(item)
	format := streamedToolCallFormat(wire)
	state, err := reducer.toolCallState(wire, format, item.ID, index)
	if err != nil {
		return tool.ReadyCall{}, err
	}
	if state.itemDone || !state.inputDone || item.CallID != state.callID || item.Name != state.name ||
		input != state.input.String() || state.outputIndex <= reducer.lastToolOutputIndex {
		return tool.ReadyCall{}, fault.New(fault.CodeStreamProtocol, "Responses completed "+format.callLabel+" is invalid")
	}
	callID, err := tool.ParseProviderCallID(state.callID)
	if err != nil {
		return tool.ReadyCall{}, fault.New(fault.CodeStreamProtocol, "Responses "+format.callLabel+" ID is invalid")
	}
	ready, err := reducer.catalog.DecodeReadyCall(domain.ProviderOpenAI, state.name, callID, []byte(input))
	if err != nil {
		return tool.ReadyCall{}, fault.Wrap(fault.CodeStreamProtocol, "Responses "+format.readyLabel+" is invalid", err)
	}
	if ready.WireKind() != wire {
		return tool.ReadyCall{}, fault.New(fault.CodeStreamProtocol, "Responses "+format.wireLabel+" wire does not match facade")
	}
	state.itemDone = true
	state.input.Reset()
	reducer.lastToolOutputIndex = state.outputIndex
	if wire == tool.WireStructured {
		reducer.pendingFunctions--
	} else {
		reducer.pendingCustoms--
	}
	return ready, nil
}

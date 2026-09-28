package anthropic

import (
	"encoding/json"
	"sort"

	"easycode/internal/codec"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/provider/transport"
)

type messagesEventEnvelope struct {
	Type         string          `json:"type"`
	Index        *int            `json:"index,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	ContentBlock json.RawMessage `json:"content_block,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	Usage        json.RawMessage `json:"usage,omitempty"`
}

type messageStartWire struct {
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	StopReason *string        `json:"stop_reason,omitempty"`
	Usage      usageEventWire `json:"usage"`
}

type contentBlockWire struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
}

type contentDeltaWire struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type messageDeltaWire struct {
	StopReason *string `json:"stop_reason,omitempty"`
}

type usageEventWire struct {
	InputTokens              *int `json:"input_tokens,omitempty"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens,omitempty"`
	OutputTokens             *int `json:"output_tokens,omitempty"`
}

type blockState struct {
	item    NativeItem
	stopped bool
}

type reducerResult struct {
	semantic *protocol.Event
	native   *NativeItem
	complete bool
}

// streamReducer 只属于单个 turn，按 block index 归并 Messages 事件。
type streamReducer struct {
	messageStarted bool
	completed      bool
	blocks         map[int]*blockState
	metadata       messageMetadata
}

func newStreamReducer() *streamReducer {
	return &streamReducer{blocks: make(map[int]*blockState)}
}

func (reducer *streamReducer) reduce(event transport.SSEEvent) (reducerResult, error) {
	if reducer.completed {
		return reducerResult{}, protocolError("Anthropic event received after message_stop")
	}

	var envelope messagesEventEnvelope
	if err := codec.Unmarshal([]byte(event.Data), &envelope); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Anthropic event is invalid JSON", err)
	}
	if envelope.Type == "" {
		return reducerResult{}, protocolError("Anthropic event type is required")
	}

	switch envelope.Type {
	case "message_start":
		return reducer.reduceMessageStart(envelope.Message)
	case "content_block_start":
		return reducer.reduceBlockStart(envelope.Index, envelope.ContentBlock)
	case "content_block_delta":
		return reducer.reduceBlockDelta(envelope.Index, envelope.Delta)
	case "content_block_stop":
		return reducer.reduceBlockStop(envelope.Index)
	case "message_delta":
		return reducer.reduceMessageDelta(envelope.Delta, envelope.Usage)
	case "message_stop":
		return reducer.reduceMessageStop()
	case "error":
		return reducerResult{}, fault.New(fault.CodeProviderRequest, "provider response failed")
	case "ping":
		return reducerResult{}, nil
	default:
		return reducerResult{}, nil
	}
}

func (reducer *streamReducer) reduceMessageStart(raw json.RawMessage) (reducerResult, error) {
	if reducer.messageStarted {
		return reducerResult{}, protocolError("Anthropic message_start is duplicated")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return reducerResult{}, protocolError("Anthropic message metadata is required")
	}
	var message messageStartWire
	if err := codec.Unmarshal(raw, &message); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Anthropic message metadata is invalid", err)
	}
	if message.ID == "" || message.Model == "" {
		return reducerResult{}, protocolError("Anthropic message ID and model are required")
	}
	reducer.messageStarted = true
	reducer.metadata.ID = message.ID
	reducer.metadata.Model = message.Model
	if message.StopReason != nil {
		reducer.metadata.StopReason = optionalString{Value: *message.StopReason, Known: true}
	}
	mergeStartUsage(&reducer.metadata.Usage, message.Usage)
	return reducerResult{}, nil
}

func (reducer *streamReducer) reduceBlockStart(index *int, raw json.RawMessage) (reducerResult, error) {
	if !reducer.messageStarted {
		return reducerResult{}, protocolError("Anthropic content block started before message_start")
	}
	resolved, err := requireBlockIndex(index)
	if err != nil {
		return reducerResult{}, err
	}
	if _, exists := reducer.blocks[resolved]; exists {
		return reducerResult{}, protocolError("Anthropic content block index is duplicated")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return reducerResult{}, protocolError("Anthropic content block is required")
	}
	var wire contentBlockWire
	if err := codec.Unmarshal(raw, &wire); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Anthropic content block is invalid", err)
	}
	item := NativeItem{Type: wire.Type}
	switch wire.Type {
	case blockTypeText:
		// start 中的 text 可能由 SDK/网关在 delta 中重复，统一只累计 delta。
	case blockTypeThinking:
		// thinking 和 signature 同样只由后续 delta 累计。
	case blockTypeRedactedThinking:
		item.RedactedData = wire.Data
		item.Raw = append(json.RawMessage(nil), raw...)
	default:
		return reducerResult{}, protocolError("Anthropic content block type is unsupported")
	}
	reducer.blocks[resolved] = &blockState{item: item}
	return reducerResult{}, nil
}

func (reducer *streamReducer) reduceBlockDelta(index *int, raw json.RawMessage) (reducerResult, error) {
	resolved, state, err := reducer.activeBlock(index)
	if err != nil {
		return reducerResult{}, err
	}
	_ = resolved
	if len(raw) == 0 || string(raw) == "null" {
		return reducerResult{}, protocolError("Anthropic content block delta is required")
	}
	var delta contentDeltaWire
	if err := codec.Unmarshal(raw, &delta); err != nil {
		return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Anthropic content block delta is invalid", err)
	}
	switch delta.Type {
	case "text_delta":
		if state.item.Type != blockTypeText {
			return reducerResult{}, protocolError("Anthropic text delta does not match content block")
		}
		state.item.Text += delta.Text
		if delta.Text == "" {
			return reducerResult{}, nil
		}
		semantic, buildErr := protocol.NewAssistantTextDelta(delta.Text)
		if buildErr != nil {
			return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Anthropic text delta is invalid", buildErr)
		}
		return reducerResult{semantic: &semantic}, nil
	case "thinking_delta":
		if state.item.Type != blockTypeThinking {
			return reducerResult{}, protocolError("Anthropic thinking delta does not match content block")
		}
		state.item.Thinking += delta.Thinking
		return reducerResult{}, nil
	case "signature_delta":
		if state.item.Type != blockTypeThinking {
			return reducerResult{}, protocolError("Anthropic signature delta does not match content block")
		}
		state.item.Signature = delta.Signature
		return reducerResult{}, nil
	default:
		return reducerResult{}, protocolError("Anthropic content block delta type is unsupported")
	}
}

func (reducer *streamReducer) reduceBlockStop(index *int) (reducerResult, error) {
	_, state, err := reducer.activeBlock(index)
	if err != nil {
		return reducerResult{}, err
	}
	state.stopped = true
	item := state.item.clone()
	return reducerResult{native: &item}, nil
}

func (reducer *streamReducer) reduceMessageDelta(deltaRaw json.RawMessage, usageRaw json.RawMessage) (reducerResult, error) {
	if !reducer.messageStarted {
		return reducerResult{}, protocolError("Anthropic message_delta received before message_start")
	}
	if len(deltaRaw) > 0 && string(deltaRaw) != "null" {
		var delta messageDeltaWire
		if err := codec.Unmarshal(deltaRaw, &delta); err != nil {
			return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Anthropic message delta is invalid", err)
		}
		if delta.StopReason != nil {
			reducer.metadata.StopReason = optionalString{Value: *delta.StopReason, Known: true}
		}
	}
	if len(usageRaw) > 0 && string(usageRaw) != "null" {
		var usage usageEventWire
		if err := codec.Unmarshal(usageRaw, &usage); err != nil {
			return reducerResult{}, fault.Wrap(fault.CodeStreamProtocol, "Anthropic message usage is invalid", err)
		}
		mergeDeltaUsage(&reducer.metadata.Usage, usage)
	}
	return reducerResult{}, nil
}

func (reducer *streamReducer) reduceMessageStop() (reducerResult, error) {
	if !reducer.messageStarted {
		return reducerResult{}, protocolError("Anthropic message_stop received before message_start")
	}
	if len(reducer.blocks) == 0 {
		return reducerResult{}, protocolError("Anthropic message_stop requires a content block")
	}
	for _, state := range reducer.blocks {
		if !state.stopped {
			return reducerResult{}, protocolError("Anthropic message_stop received with an active content block")
		}
	}
	reducer.completed = true
	return reducerResult{complete: true}, nil
}

func (reducer *streamReducer) activeBlock(index *int) (int, *blockState, error) {
	resolved, err := requireBlockIndex(index)
	if err != nil {
		return 0, nil, err
	}
	state, exists := reducer.blocks[resolved]
	if !exists {
		return 0, nil, protocolError("Anthropic content block has not started")
	}
	if state.stopped {
		return 0, nil, protocolError("Anthropic content block is already stopped")
	}
	return resolved, state, nil
}

func (reducer *streamReducer) assistantMessage() nativeMessage {
	indexes := make([]int, 0, len(reducer.blocks))
	for index := range reducer.blocks {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	message := nativeMessage{Role: roleAssistant, Content: make([]NativeItem, 0, len(indexes))}
	for _, index := range indexes {
		message.Content = append(message.Content, reducer.blocks[index].item.clone())
	}
	return message
}

func (reducer *streamReducer) messageMetadata() messageMetadata {
	return reducer.metadata.clone()
}

func requireBlockIndex(index *int) (int, error) {
	if index == nil || *index < 0 {
		return 0, protocolError("Anthropic content block index is invalid")
	}
	return *index, nil
}

func mergeStartUsage(target *rawUsage, wire usageEventWire) {
	setKnownInt(&target.InputTokens, wire.InputTokens)
	setKnownInt(&target.CacheCreationInputTokens, wire.CacheCreationInputTokens)
	setKnownInt(&target.CacheReadInputTokens, wire.CacheReadInputTokens)
	setKnownInt(&target.OutputTokens, wire.OutputTokens)
}

func mergeDeltaUsage(target *rawUsage, wire usageEventWire) {
	setPositiveInt(&target.InputTokens, wire.InputTokens)
	setPositiveInt(&target.CacheCreationInputTokens, wire.CacheCreationInputTokens)
	setPositiveInt(&target.CacheReadInputTokens, wire.CacheReadInputTokens)
	setKnownInt(&target.OutputTokens, wire.OutputTokens)
}

func setKnownInt(target *optionalInt, value *int) {
	if value != nil {
		*target = optionalInt{Value: *value, Known: true}
	}
}

func setPositiveInt(target *optionalInt, value *int) {
	if value != nil && *value > 0 {
		*target = optionalInt{Value: *value, Known: true}
	}
}

func protocolError(message string) error {
	return fault.New(fault.CodeStreamProtocol, message)
}

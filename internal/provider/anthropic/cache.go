package anthropic

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"encoding/json"
	"fmt"
)

type cacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}
type cachedBlock struct {
	item   NativeItem
	marker *cacheControl
}

func (block cachedBlock) MarshalJSON() ([]byte, error) {
	original, err := block.item.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if block.marker == nil && len(block.item.Raw) == 0 {
		return original, nil
	}
	members, err := codec.ObjectMembers(original)
	if err != nil {
		return nil, err
	}
	if _, exists := members["cache_control"]; exists {
		return nil, fmt.Errorf("native cache control conflicts with request policy")
	}
	if block.marker == nil {
		return original, nil
	}
	marker, err := codec.MarshalStable(block.marker)
	if err != nil {
		return nil, err
	}
	members["cache_control"] = json.RawMessage(marker)
	return codec.MarshalStable(members)
}

type cachedMessage struct {
	Role    string        `json:"role"`
	Content []cachedBlock `json:"content"`
}
type cachedTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}
type cachedMessagesRequest struct {
	Thinking     *thinkingWire      `json:"thinking,omitempty"`
	OutputConfig *outputConfigWire  `json:"output_config,omitempty"`
	ToolChoice   *summaryToolChoice `json:"tool_choice,omitempty"`
	Model        string             `json:"model"`
	Messages     []cachedMessage    `json:"messages"`
	MaxTokens    int                `json:"max_tokens"`
	Tools        []cachedTool       `json:"tools"`
	Stream       bool               `json:"stream"`
}
type cachePosition struct {
	message int
	block   int
	ordinal int
}

func cacheable(item NativeItem) bool {
	switch item.Type {
	case blockTypeText, blockTypeToolUse, blockTypeToolResult:
		return true
	default:
		return false
	}
}
func messageCachePositions(plan samplingRequestPlan) []cachePosition {
	messages := plan.request.Messages
	end := len(messages)
	if plan.purpose == domain.RequestSummary {
		end--
	}

	prefixEnd := -1
	if plan.project || plan.skills {
		prefixEnd = 0
		if plan.project && plan.skills {
			prefixEnd = 1
		}
	}
	bridgeEnd := plan.request.lastSampleMessage - 1
	prefix, bridge, tail := cachePosition{ordinal: -1}, cachePosition{ordinal: -1}, cachePosition{ordinal: -1}
	ordinal := 0
	for i, message := range messages[:max(end, 0)] {
		for j, item := range message.Content {
			if cacheable(item) {
				tail = cachePosition{message: i, block: j, ordinal: ordinal}
			}
			ordinal++
		}
		switch i {
		case prefixEnd, bridgeEnd:
			if i == prefixEnd {
				prefix = tail
			}
			if i == bridgeEnd {
				bridge = tail
			}
		}
	}
	positions := make([]cachePosition, 0, 3)
	if prefix.ordinal >= 0 {
		positions = append(positions, prefix)
	}
	if bridge.ordinal >= 0 && bridge.ordinal != prefix.ordinal && tail.ordinal-bridge.ordinal >= 20 {
		positions = append(positions, bridge)
	}
	if tail.ordinal >= 0 && tail.ordinal != prefix.ordinal {
		positions = append(positions, tail)
	}
	return positions
}

type compiledMessagesRequest struct {
	body    codec.CanonicalJSON
	markers uint8
}

func (plan samplingRequestPlan) compile(mode domain.AnthropicCacheMode) (compiledMessagesRequest, error) {
	if mode == domain.AnthropicCacheDisabled {
		body, err := codec.MarshalCanonical(plan.request, maxMessagesRequestBytes)
		return compiledMessagesRequest{body: body}, err
	}
	if mode != domain.AnthropicCache5m && mode != domain.AnthropicCache1h {
		return compiledMessagesRequest{}, fmt.Errorf("invalid Anthropic cache policy")
	}
	marker := cacheControl{Type: "ephemeral"}
	if mode == domain.AnthropicCache1h {
		marker.TTL = "1h"
	}
	r := plan.request
	wire := cachedMessagesRequest{Thinking: plan.request.Thinking, OutputConfig: plan.request.OutputConfig, ToolChoice: r.ToolChoice, Model: r.Model, MaxTokens: r.MaxTokens, Stream: r.Stream, Messages: make([]cachedMessage, len(r.Messages)), Tools: make([]cachedTool, len(r.Tools))}
	for i, m := range r.Messages {
		wire.Messages[i] = cachedMessage{Role: m.Role, Content: make([]cachedBlock, len(m.Content))}
		for j, item := range m.Content {
			wire.Messages[i].Content[j] = cachedBlock{item: item}
		}
	}
	for i, t := range r.Tools {
		wire.Tools[i] = cachedTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}
	}
	positions := messageCachePositions(plan)
	markers := len(positions)
	if len(wire.Tools) > 0 {
		wire.Tools[len(wire.Tools)-1].CacheControl = &marker
		markers++
	}
	for _, p := range positions {
		wire.Messages[p.message].Content[p.block].marker = &marker
	}
	body, err := codec.MarshalCanonical(wire, maxMessagesRequestBytes)
	return compiledMessagesRequest{body: body, markers: uint8(markers)}, err
}

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
	ToolChoice *summaryToolChoice `json:"tool_choice,omitempty"`
	Model      string             `json:"model"`
	Messages   []cachedMessage    `json:"messages"`
	MaxTokens  int                `json:"max_tokens"`
	Tools      []cachedTool       `json:"tools"`
	Stream     bool               `json:"stream"`
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
func lastCachePosition(messages []nativeMessage, end int) (cachePosition, bool) {
	ordinal := 0
	var result cachePosition
	found := false
	for i, message := range messages {
		if i > end {
			break
		}
		for j, item := range message.Content {
			if cacheable(item) {
				result = cachePosition{i, j, ordinal}
				found = true
			}
			ordinal++
		}
	}
	return result, found
}
func messageCachePositions(plan samplingRequestPlan, summary bool) []cachePosition {
	messages := plan.request.Messages
	end := len(messages) - 1
	if summary {
		end--
	}
	positions := make([]cachePosition, 0, 3)
	appendUnique := func(p cachePosition) {
		for _, old := range positions {
			if old.message == p.message && old.block == p.block {
				return
			}
		}
		positions = append(positions, p)
	}
	if plan.project {
		if p, ok := lastCachePosition(messages, 0); ok {
			appendUnique(p)
		}
	}
	tail, exists := lastCachePosition(messages, end)
	if !exists {
		return positions
	}
	messageCount := 0
	if plan.project {
		messageCount++
	}
	boundary := -1
	for _, entry := range plan.history {
		messageCount += len(entry.UserInputs)
		switch entry.Kind {
		case nativeHistorySample:
			boundary = messageCount - 1
			messageCount++
		case nativeHistoryToolOutputs:
			messageCount++
		}
	}
	if boundary >= 0 {
		if bridge, ok := lastCachePosition(messages, boundary); ok && tail.ordinal-bridge.ordinal >= 20 {
			appendUnique(bridge)
		}
	}
	appendUnique(tail)
	return positions
}
func compileCachedMessagesRequest(plan samplingRequestPlan, summary bool, mode domain.AnthropicCacheMode) (codec.CanonicalJSON, error) {
	if mode == domain.AnthropicCacheDisabled {
		return codec.MarshalCanonical(plan.request, maxMessagesRequestBytes)
	}
	if mode != domain.AnthropicCache5m && mode != domain.AnthropicCache1h {
		return codec.CanonicalJSON{}, fmt.Errorf("invalid Anthropic cache policy")
	}
	marker := cacheControl{Type: "ephemeral"}
	if mode == domain.AnthropicCache1h {
		marker.TTL = "1h"
	}
	r := plan.request
	wire := cachedMessagesRequest{ToolChoice: r.ToolChoice, Model: r.Model, MaxTokens: r.MaxTokens, Stream: r.Stream, Messages: make([]cachedMessage, len(r.Messages)), Tools: make([]cachedTool, len(r.Tools))}
	for i, m := range r.Messages {
		wire.Messages[i] = cachedMessage{Role: m.Role, Content: make([]cachedBlock, len(m.Content))}
		for j, item := range m.Content {
			wire.Messages[i].Content[j] = cachedBlock{item: item.clone()}
		}
	}
	for i, t := range r.Tools {
		wire.Tools[i] = cachedTool{Name: t.Name, Description: t.Description, InputSchema: append(json.RawMessage(nil), t.InputSchema...)}
	}
	if len(wire.Tools) > 0 {
		wire.Tools[len(wire.Tools)-1].CacheControl = &marker
	}
	for _, p := range messageCachePositions(plan, summary) {
		wire.Messages[p.message].Content[p.block].marker = &marker
	}
	return codec.MarshalCanonical(wire, maxMessagesRequestBytes)
}

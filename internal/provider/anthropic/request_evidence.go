package anthropic

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

func (plan samplingRequestPlan) evidence() (provider.RequestEvidence, error) {
	request := plan.request
	input := request.Messages
	request.Messages = nil
	return provider.CompileRequestEvidence(input, request, "messages", maxMessagesRequestBytes)
}

func (conversation *Conversation) describeRequest(request codec.CanonicalJSON, e provider.RequestEvidence, purpose domain.RequestPurpose) (domain.RequestReceipt, error) {
	markers, err := requestMarkerCount(request, conversation.provider.config.CachePolicy.Anthropic())
	if err != nil {
		return domain.RequestReceipt{}, err
	}
	return e.Receipt(request, conversation.baseline, conversation.nextReason, domain.RequestReceipt{
		Family: conversation.Family(), Purpose: purpose,
		Policy: conversation.provider.config.CachePolicy, Markers: markers,
	})
}

func requestMarkerCount(request codec.CanonicalJSON, mode domain.AnthropicCacheMode) (uint8, error) {
	if mode == domain.AnthropicCacheDisabled {
		return 0, nil
	}
	var wire struct {
		Messages []nativeMessage `json:"messages"`
		Tools    []struct {
			CacheControl *cacheControl `json:"cache_control"`
		} `json:"tools"`
	}
	if err := codec.Unmarshal(request.Bytes(), &wire); err != nil {
		return 0, err
	}
	count := 0
	for _, tool := range wire.Tools {
		if tool.CacheControl != nil {
			count++
		}
	}
	for _, message := range wire.Messages {
		for _, item := range message.Content {
			if !cacheable(item) {
				continue
			}
			members, err := codec.ObjectMembers(item.Raw)
			if err != nil {
				return 0, err
			}
			if _, exists := members["cache_control"]; exists {
				count++
			}
		}
	}
	if count > 4 {
		return 0, fmt.Errorf("cache marker limit exceeded")
	}
	return uint8(count), nil
}

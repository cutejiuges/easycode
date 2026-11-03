package anthropic

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"encoding/json"
	"fmt"
)

type requestEvidence struct {
	items             []codec.CanonicalJSON
	properties        codec.CanonicalJSON
	nativeFingerprint string
}

func (conversation *Conversation) requestEvidence(input provider.TurnInput) (requestEvidence, error) {
	plan, err := conversation.planSamplingRequest(conversation.history.snapshot(), input)
	if err != nil {
		return requestEvidence{}, err
	}
	request := plan.request
	items := make([]codec.CanonicalJSON, 0, len(request.Messages))
	raw := make([]json.RawMessage, 0, len(request.Messages))
	for _, item := range request.Messages {
		part, e := codec.MarshalCanonical(item, provider.MaxNativeCommitBytes)
		if e != nil {
			return requestEvidence{}, e
		}
		items = append(items, part)
		raw = append(raw, part.Bytes())
	}
	native, err := codec.MarshalCanonical(raw, provider.MaxNativeCommitBytes)
	if err != nil {
		return requestEvidence{}, err
	}
	request.Messages = nil
	properties, err := codec.MarshalCanonical(request, maxMessagesRequestBytes)
	if err != nil {
		return requestEvidence{}, err
	}
	return requestEvidence{items, properties, domain.ContentFingerprint(native.Bytes())}, nil
}
func (e requestEvidence) commitBaseline(entry nativeHistoryEntry) (provider.RequestBaseline, error) {
	output, err := codec.MarshalCanonical(entry.Assistant, provider.MaxNativeCommitBytes)
	if err != nil {
		return provider.RequestBaseline{}, err
	}
	return provider.NewRequestBaseline(e.items, []codec.CanonicalJSON{output}, e.properties)
}
func (conversation *Conversation) describeRequest(request codec.CanonicalJSON, e requestEvidence, purpose domain.RequestPurpose) (domain.RequestReceipt, error) {
	_, relation, reason := conversation.baseline.Compare(e.items, e.properties)
	if conversation.nextReason != "" && conversation.nextReason != domain.ResetNone {
		reason = conversation.nextReason
		if reason == domain.ResetCompacted {
			relation = domain.NativeBoundary
		}
	}
	if purpose == domain.RequestSummary {
		reason = domain.ResetSummary
		relation = domain.NativeBoundary
	}
	markers, err := requestMarkerCount(request, conversation.provider.config.CachePolicy.Anthropic())
	if err != nil {
		return domain.RequestReceipt{}, err
	}
	return domain.NewRequestReceipt(domain.RequestReceipt{Family: domain.ProviderAnthropic, Purpose: purpose, Policy: conversation.provider.config.CachePolicy, RequestFingerprint: domain.ContentFingerprint(request.Bytes()), NativeFingerprint: e.nativeFingerprint, Relation: relation, Transport: domain.TransportHTTP, Mode: domain.RequestFull, Reason: reason, Fallback: domain.FallbackNone, Markers: uint8(markers)})
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

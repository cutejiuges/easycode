package openai

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"encoding/json"
)

type requestEvidence struct {
	items             []codec.CanonicalJSON
	properties        codec.CanonicalJSON
	nativeFingerprint string
}

func (plan samplingRequestPlan) evidence() (requestEvidence, error) {
	request := plan.request
	items := make([]codec.CanonicalJSON, 0, len(request.Input))
	raw := make([]json.RawMessage, 0, len(request.Input))
	for _, item := range request.Input {
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
	request.Input = nil
	properties, err := codec.MarshalCanonical(request, maxResponsesRequestBytes)
	if err != nil {
		return requestEvidence{}, err
	}
	members, err := codec.ObjectMembers(properties.Bytes())
	if err != nil {
		return requestEvidence{}, err
	}
	delete(members, "input")
	properties, err = codec.MarshalCanonical(members, maxResponsesRequestBytes)
	if err != nil {
		return requestEvidence{}, err
	}
	return requestEvidence{items, properties, domain.ContentFingerprint(native.Bytes())}, nil
}
func (e requestEvidence) commitBaseline(entry nativeHistoryEntry) (provider.RequestBaseline, error) {
	outputs := make([]codec.CanonicalJSON, 0, len(entry.Outputs))
	for _, item := range entry.Outputs {
		part, err := codec.MarshalCanonical(item, provider.MaxNativeCommitBytes)
		if err != nil {
			return provider.RequestBaseline{}, err
		}
		outputs = append(outputs, part)
	}
	return provider.NewRequestBaseline(e.items, outputs, e.properties)
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
	return domain.NewRequestReceipt(domain.RequestReceipt{Family: domain.ProviderOpenAI, Purpose: purpose, Policy: conversation.provider.config.CachePolicy, RequestFingerprint: domain.ContentFingerprint(request.Bytes()), NativeFingerprint: e.nativeFingerprint, Relation: relation, Transport: domain.TransportHTTP, Mode: domain.RequestFull, Reason: reason, Fallback: domain.FallbackNone})
}

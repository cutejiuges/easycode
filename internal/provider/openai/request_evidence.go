package openai

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

func (plan samplingRequestPlan) evidence() (provider.RequestEvidence, error) {
	request := plan.request
	input := request.Input
	request.Input = nil
	return provider.CompileRequestEvidence(input, request, "input", maxResponsesRequestBytes)
}

func (conversation *Conversation) describeRequest(request codec.CanonicalJSON, e provider.RequestEvidence, purpose domain.RequestPurpose) (domain.RequestReceipt, error) {
	return e.Receipt(request, conversation.baseline, conversation.nextReason, domain.RequestReceipt{
		Family: conversation.Family(), Purpose: purpose, Policy: conversation.provider.config.CachePolicy,
	})
}

package anthropic

import (
	"easycode/internal/domain"
	"easycode/internal/provider"
)

func (plan samplingRequestPlan) evidence() (provider.RequestEvidence, error) {
	request := plan.request
	input := request.Messages
	request.Messages = nil
	return provider.CompileRequestEvidence(input, request, "messages", maxMessagesRequestBytes)
}

func (conversation *Conversation) describeRequest(request compiledMessagesRequest, e provider.RequestEvidence, purpose domain.RequestPurpose) (domain.RequestReceipt, error) {
	return e.Receipt(request.body, e.Compare(conversation.baseline), conversation.nextReason, domain.RequestReceipt{
		Family: conversation.Family(), Purpose: purpose,
		Policy: conversation.provider.config.CachePolicy, Markers: request.markers,
	})
}

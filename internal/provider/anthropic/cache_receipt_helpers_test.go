package anthropic

import "easycode/internal/domain"

// syntheticRequestReceipt 为不经过真实网络的测试提供明确收据。
func syntheticRequestReceipt(purpose domain.RequestPurpose) domain.RequestReceipt {
	reason := domain.ResetFirst
	if purpose == domain.RequestSummary {
		reason = domain.ResetSummary
	}
	return domain.RequestReceipt{Family: domain.ProviderAnthropic, Purpose: purpose, Policy: domain.DefaultCachePolicy(), RequestFingerprint: domain.ContentFingerprint([]byte("test-request")), NativeFingerprint: domain.ContentFingerprint([]byte("test-native")), Relation: domain.NativeFirst, Transport: domain.TransportHTTP, Mode: domain.RequestFull, Reason: reason, Fallback: domain.FallbackNone}
}

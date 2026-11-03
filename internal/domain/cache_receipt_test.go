package domain

import "testing"

func TestRequestReceiptRejectsInvalidCombinations(t *testing.T) {
	valid := RequestReceipt{Family: ProviderOpenAI, Purpose: RequestSample, Policy: DefaultCachePolicy(), RequestFingerprint: ContentFingerprint([]byte("request")), NativeFingerprint: ContentFingerprint([]byte("native")), Relation: NativeFirst, Transport: TransportHTTP, Mode: RequestFull, Reason: ResetFirst, Fallback: FallbackNone}
	receipt, err := NewRequestReceipt(valid)
	if err != nil {
		t.Fatal(err)
	}
	copy := receipt
	copy.RequestFingerprint = "invalid"
	if receipt.RequestFingerprint == copy.RequestFingerprint {
		t.Fatal("receipt aliases caller state")
	}
	mutations := []func(*RequestReceipt){func(r *RequestReceipt) { r.Family = "unknown" }, func(r *RequestReceipt) { r.Purpose = "" }, func(r *RequestReceipt) { r.Policy = CachePolicy{} }, func(r *RequestReceipt) { r.RequestFingerprint = "" }, func(r *RequestReceipt) { r.NativeFingerprint = "invalid" }, func(r *RequestReceipt) { r.Relation = "unknown" }, func(r *RequestReceipt) { r.Transport = "unknown" }, func(r *RequestReceipt) { r.Mode = RequestIncremental }, func(r *RequestReceipt) { r.Reason = "unknown" }, func(r *RequestReceipt) { r.Fallback = FallbackHandshake }, func(r *RequestReceipt) { r.Markers = 1 }}
	for i, mutate := range mutations {
		r := valid
		mutate(&r)
		if _, err = NewRequestReceipt(r); err == nil {
			t.Fatal("invalid receipt accepted", i)
		}
	}
	if _, err = NewRequestReceipt(RequestReceipt{}); err == nil {
		t.Fatal("zero receipt accepted")
	}
}

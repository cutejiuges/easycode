package domain

import "testing"

func TestCachePolicyValidationAndIdentity(t *testing.T) {
	if (CachePolicy{}).Validate() == nil {
		t.Fatal("zero policy accepted")
	}
	p := DefaultCachePolicy()
	if p.Validate() != nil || p.ValidateFamily(ProviderOpenAI) != nil || p.ValidateFamily(ProviderAnthropic) != nil {
		t.Fatal("default policy rejected")
	}
	for _, v := range []struct {
		a AnthropicCacheMode
		k OpenAICacheKeyMode
		w OpenAITransportMode
	}{{"", OpenAICacheOmit, OpenAIHTTPSSE}, {"unknown", OpenAICacheOmit, OpenAIHTTPSSE}, {AnthropicCacheDisabled, "unknown", OpenAIHTTPSSE}, {AnthropicCacheDisabled, OpenAICacheOmit, "unknown"}} {
		if _, err := NewCachePolicy(v.a, v.k, v.w); err == nil {
			t.Fatal("invalid policy accepted", v)
		}
	}
	a, _ := NewCachePolicy(AnthropicCache5m, OpenAICacheOmit, OpenAIHTTPSSE)
	b, _ := NewCachePolicy(AnthropicCache1h, OpenAICacheOmit, OpenAIHTTPSSE)
	if a.ValidateFamily(ProviderOpenAI) == nil || b.ValidateFamily(ProviderAnthropic) != nil {
		t.Fatal("family boundary lost")
	}
	af, _ := a.Fingerprint()
	bf, _ := b.Fingerprint()
	if af == bf {
		t.Fatal("TTL change hidden")
	}
	id := ThreadID("01900000-0000-7000-8000-000000000001")
	key, err := ThreadCacheKey(id)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := ThreadCacheKey(id)
	other, _ := ThreadCacheKey("01900000-0000-7000-8000-000000000002")
	if key != again || key == other || len(key) != 73 {
		t.Fatal("key identity unstable")
	}
	if _, err = ThreadCacheKey(""); err == nil {
		t.Fatal("empty ID accepted")
	}
	if p.Key() != OpenAICacheOmit || p.Anthropic() != AnthropicCacheDisabled || p.Transport() != OpenAIHTTPSSE {
		t.Fatal("default changed")
	}
}

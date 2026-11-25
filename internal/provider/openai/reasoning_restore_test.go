package openai

import (
	"os"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestReasoningFixedNativeRestoreAndCacheBytesIgnoreReading(t *testing.T) {
	fixture, err := os.ReadFile("testdata/reasoning_restore.current.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []domain.OpenAICacheKeyMode{domain.OpenAICacheOmit, domain.OpenAICacheThread} {
		policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, key, domain.OpenAIHTTPSSE)
		settings, _ := domain.NewOpenAIInference("high", "auto", 16384)
		instance, err := New(Config{Inference: settings, BaseURL: "https://example.invalid", Model: "gpt-test", APIKey: secret.New("fixture-key"), CachePolicy: policy})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer instance.Close()
			live := testConversation(t, instance)
			envelopes := testsupport.LoadNativeFixture(t, live, fixture)
			restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, envelopes))
			if err != nil {
				t.Fatal(err)
			}
			restored := restoredValue.(*Conversation)
			input, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			testsupport.CheckReasoningRestore(t, live, restored, input, 3)
		}()
	}
}

func (c *Conversation) TestCacheSources(t *testing.T) contextplan.CacheSources {
	t.Helper()
	return websocketCacheSources(t, c)
}

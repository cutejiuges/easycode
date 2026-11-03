package openai

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"os"
	"testing"
)

func TestThreadCacheKeySurvivesNativeRestore(t *testing.T) {
	policy, err := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIHTTPSSE)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := New(Config{Model: "fixture-model", BaseURL: "https://example.invalid/prefix", APIKey: secret.New("sentinel-secret"), CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if _, err = instance.NewConversation(""); err == nil {
		t.Fatal("invalid identity accepted")
	}
	if _, err = instance.RestoreConversation("", nil); err == nil {
		t.Fatal("invalid restored identity accepted")
	}
	conversation := testConversation(t, instance)
	testCommitUserInput(t, conversation, "  preserved text\n")
	input, err := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	live, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	var request responsesRequest
	if err = codec.Unmarshal(live.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	expected, _ := domain.ThreadCacheKey(conversation.threadID)
	if request.PromptCacheKey != expected || bytes.Contains(live.Bytes(), []byte("sentinel-secret")) {
		t.Fatal("incorrect key or leaked secret")
	}
	entries := conversation.history.snapshot()
	records := make([]provider.RestoreRecord, 0, len(entries))
	for _, entry := range entries {
		envelope, encodeErr := encodeNativeCommit(entry)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		records = append(records, testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope})...)
	}
	value, err := instance.RestoreConversation(conversation.threadID, records)
	if err != nil {
		t.Fatal(err)
	}
	restored := value.(*Conversation)
	next, err := restored.compileSamplingRequest(restored.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live.Bytes(), next.Bytes()) {
		t.Fatal("restored request differs")
	}
	another, err := instance.NewConversation("01900000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	other := another.(*Conversation)
	other.history.entries = entries
	different, err := other.compileSamplingRequest(other.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(live.Bytes(), different.Bytes()) {
		t.Fatal("thread keys not isolated")
	}
}

func TestOpenAICacheRequestGolden(t *testing.T) {
	policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIHTTPSSE)
	instance, err := New(Config{Model: "gpt-test", BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	testCommitUserInput(t, conversation, "hello")
	input, err := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/cache_thread_request.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), bytes.TrimSpace(want)) {
		t.Fatal("thread cache golden differs")
	}
}

func TestCacheCapabilitiesFollowExplicitPolicyAndFallback(t *testing.T) {
	for _, key := range []domain.OpenAICacheKeyMode{domain.OpenAICacheOmit, domain.OpenAICacheThread} {
		for _, mode := range []domain.OpenAITransportMode{domain.OpenAIHTTPSSE, domain.OpenAIWebSocketAuto} {
			policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, key, mode)
			p, err := New(Config{Model: "model", BaseURL: "https://example.invalid", APIKey: secret.New("key"), CachePolicy: policy})
			if err != nil {
				t.Fatal(err)
			}
			c := testConversation(t, p)
			caps := c.Capabilities()
			if caps.PromptCacheKey != (key == domain.OpenAICacheThread) || caps.PreviousResponse != (mode == domain.OpenAIWebSocketAuto) || caps.PromptCacheControl || caps.ReasoningSummary || caps.ParallelToolCalls {
				t.Fatal(caps)
			}
			c.httpFallback.Store(true)
			if c.Capabilities().PreviousResponse {
				t.Fatal("fallback kept incremental capability")
			}
			_ = p.Close()
		}
	}
}

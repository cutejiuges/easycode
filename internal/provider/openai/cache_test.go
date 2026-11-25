package openai

import (
	"bytes"
	"os"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider/testsupport"
	"easycode/internal/secret"
)

func TestOpenAICacheRequestGolden(t *testing.T) {
	policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, domain.OpenAICacheThread, domain.OpenAIHTTPSSE)
	instance, err := New(Config{Model: "gpt-test", BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), CachePolicy: policy})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	testCommitUserInput(t, conversation, "hello")
	input, err := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := conversation.compileSamplingRequest(conversation.history.Entries(), input)
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

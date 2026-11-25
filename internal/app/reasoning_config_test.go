package app

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
)

func withReasoningConfig(t *testing.T, path, fields string) string {
	t.Helper()
	data := readAppTestFile(t, path)
	body := strings.TrimSuffix(strings.TrimSpace(string(data)), "}") + "," + fields + "}"
	writeAppTestFile(t, path, []byte(body), 0600)
	return path
}

func reasoningFields(family domain.ProviderFamily) string {
	if family == domain.ProviderAnthropic {
		return `"anthropic_thinking":{"type":"adaptive","display":"omitted"},"anthropic_effort":"high","max_output_tokens":16384`
	}
	return `"openai_reasoning_effort":"high","openai_reasoning_summary":"auto","max_output_tokens":16384`
}

func reasoningFixtureConfig(t *testing.T, original config.Config) config.Config {
	t.Helper()
	policy := original.CachePolicy()
	path := writeAppConfig(t, t.TempDir(), original.Provider.Family, original.Provider.BaseURL)
	fields := reasoningFields(original.Provider.Family) + fmt.Sprintf(`,"anthropic_prompt_cache":%q,"openai_prompt_cache_key":%q,"openai_transport":%q`, policy.Anthropic(), policy.Key(), policy.Transport())
	loaded, err := config.Load(withReasoningConfig(t, path, fields))
	if err != nil {
		t.Fatal(err)
	}
	loaded.Provider = original.Provider
	loaded.ContextBudget = original.ContextBudget
	if err := loaded.ValidateProvider(); err != nil {
		t.Fatal(err)
	}
	return loaded
}

func assertReasoningRequest(t *testing.T, family domain.ProviderFamily, request []byte, summary bool) {
	t.Helper()
	fields := []string{`"reasoning":{"effort":"high","summary":"auto"}`, `"max_output_tokens":16384`}
	if summary {
		fields[1] = `"max_output_tokens":4096`
	}
	if family == domain.ProviderAnthropic {
		fields = []string{`"thinking":{"type":"adaptive","display":"omitted"}`, `"output_config":{"effort":"high"}`, `"max_tokens":16384`}
		if summary {
			fields[2] = `"max_tokens":4096`
		}
	}
	for _, field := range fields {
		if !bytes.Contains(request, []byte(field)) {
			t.Fatalf("missing actual frozen field %s", field)
		}
	}
}

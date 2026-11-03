package config

import (
	"easycode/internal/domain"
	"testing"
)

func TestCacheConfigurationStrictValues(t *testing.T) {
	for _, value := range []string{`null`, `true`, `0`, `""`, `"EPHEMERAL_5M"`, `"ephemeral_5m "`, `"unknown"`} {
		if _, err := decodeFile([]byte(`{"anthropic_prompt_cache":` + value + `}`)); err == nil {
			t.Fatal("invalid value accepted", value)
		}
	}
	for _, body := range []string{`{"openai_transport":null}`, `{"openai_prompt_cache_key":"custom"}`, `{"openai_transport":"http_sse","openai_transport":"websocket_auto"}`, `{"Anthropic_prompt_cache":"disabled"}`} {
		if _, err := decodeFile([]byte(body)); err == nil {
			t.Fatal("invalid config accepted", body)
		}
	}
	values, err := decodeFile([]byte(`{"provider":"anthropic","anthropic_prompt_cache":"ephemeral_1h"}`))
	if err != nil {
		t.Fatal(err)
	}
	config := values.toConfig()
	if config.CachePolicy().Anthropic() != domain.AnthropicCache1h {
		t.Fatal("TTL not retained")
	}
	t.Setenv("EASYCODE_PROVIDER", "openai")
	config = mergeEnvironment(config)
	if config.CachePolicy().ValidateFamily(config.Provider.Family) == nil {
		t.Fatal("environment family mismatch accepted")
	}
	defaults := (Config{}).CachePolicy()
	if defaults != domain.DefaultCachePolicy() {
		t.Fatal("default changed")
	}
	values, err = decodeFile([]byte(`{"provider":"openai","openai_prompt_cache_key":"thread","openai_transport":"websocket_auto"}`))
	if err != nil {
		t.Fatal(err)
	}
	config = values.toConfig()
	if err = config.CachePolicy().ValidateFamily(config.Provider.Family); err != nil {
		t.Fatal(err)
	}
}

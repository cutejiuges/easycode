package app

import (
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
)

func cacheFixtureConfig(t *testing.T, original config.Config, enabled bool) config.Config {
	return cacheFixtureConfigMode(t, original, enabled, "http_sse")
}
func cacheFixtureConfigMode(t *testing.T, original config.Config, enabled bool, transport string) config.Config {
	t.Helper()
	if !enabled {
		return original
	}
	anthropic, key := "disabled", "thread"
	if original.Provider.Family == domain.ProviderAnthropic {
		anthropic = "ephemeral_1h"
		key = "omit"
	}
	body, err := codec.MarshalStable(struct {
		Provider  domain.ProviderFamily `json:"provider"`
		BaseURL   string                `json:"base_url"`
		APIKey    string                `json:"api_key"`
		Model     string                `json:"model"`
		Anthropic string                `json:"anthropic_prompt_cache"`
		Key       string                `json:"openai_prompt_cache_key"`
		Transport string                `json:"openai_transport"`
	}{original.Provider.Family, original.Provider.BaseURL, original.Provider.APIKey.Reveal(), original.Provider.Model, anthropic, key, transport})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// 缓存配置走真实 loader，但本地模拟服务地址和假凭据不允许被宿主环境覆盖。
	loaded.Provider = original.Provider
	loaded.ContextBudget = original.ContextBudget
	return loaded
}

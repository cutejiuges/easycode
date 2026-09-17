package config

import (
	"strings"
	"testing"
)

func TestLoadFromEnvDoesNotExposeAPIKey(t *testing.T) {
	t.Setenv("EASYCODE_PROVIDER", "anthropic")
	t.Setenv("EASYCODE_BASE_URL", "https://user:password@example.com/v1?token=url-secret")
	t.Setenv("EASYCODE_API_KEY", "top-secret")
	t.Setenv("EASYCODE_MODEL", "example-model")

	loaded := LoadFromEnv()
	if err := loaded.ValidateProvider(); err != nil {
		t.Fatalf("validate provider: %v", err)
	}
	if strings.Contains(loaded.String(), "top-secret") {
		t.Fatal("configuration summary leaked API key")
	}
	if strings.Contains(loaded.String(), "password") || strings.Contains(loaded.String(), "url-secret") {
		t.Fatal("configuration summary leaked base URL credentials")
	}
}

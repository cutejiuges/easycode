package config

import (
	"errors"
	"strings"
	"testing"

	"easycode/internal/fault"
)

func TestLoadFromEnvValidatesProviderWithoutExposingAPIKey(t *testing.T) {
	t.Setenv("EASYCODE_PROVIDER", "openai")
	t.Setenv("EASYCODE_BASE_URL", "https://example.com/openai/v1")
	t.Setenv("EASYCODE_API_KEY", "top-secret")
	t.Setenv("EASYCODE_MODEL", "example-model")

	loaded := LoadFromEnv()
	if err := loaded.ValidateProvider(); err != nil {
		t.Fatalf("validate provider: %v", err)
	}
	if strings.Contains(loaded.String(), "top-secret") {
		t.Fatal("configuration summary leaked API key")
	}
}

func TestValidateProviderRejectsUnsafeBaseURL(t *testing.T) {
	tests := []string{
		"https://user:password@example.com/v1",
		"https://example.com/v1?token=url-secret",
		"https://example.com/v1#fragment",
		"ftp://example.com/v1",
		"not-a-url",
	}
	for _, baseURL := range tests {
		t.Run(baseURL, func(t *testing.T) {
			t.Setenv("EASYCODE_PROVIDER", "openai")
			t.Setenv("EASYCODE_BASE_URL", baseURL)
			t.Setenv("EASYCODE_API_KEY", "top-secret")
			t.Setenv("EASYCODE_MODEL", "example-model")
			err := LoadFromEnv().ValidateProvider()
			if !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
				t.Fatalf("base URL %q error: %v", baseURL, err)
			}
			if strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "url-secret") {
				t.Fatalf("unsafe validation error: %v", err)
			}
		})
	}
}

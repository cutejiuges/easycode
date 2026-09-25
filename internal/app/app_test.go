package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/secret"
)

func TestRunPrintModeRemainsUnimplemented(t *testing.T) {
	var output bytes.Buffer
	err := Run(context.Background(), Options{Headless: true, Output: &output})
	if !errors.Is(err, &fault.Error{Code: fault.CodeNotImplemented}) {
		t.Fatalf("print mode error: %v", err)
	}
	if strings.Contains(output.String(), "scaffold") {
		t.Fatalf("legacy scaffold output remains: %q", output.String())
	}
}

func TestNewChatResourcesRejectsMissingConfiguration(t *testing.T) {
	_, err := newChatResources(config.Config{})
	if !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
		t.Fatalf("missing configuration error: %v", err)
	}
}

func TestNewChatResourcesRejectsAnthropicWithoutLeakingSecret(t *testing.T) {
	resources, err := newChatResources(config.Config{Provider: config.Provider{
		Family:  domain.ProviderAnthropic,
		BaseURL: "https://example.com/v1",
		APIKey:  secret.New("top-secret"),
		Model:   "claude-test",
	}})
	if resources != nil || !errors.Is(err, &fault.Error{Code: fault.CodeProviderUnavailable}) {
		t.Fatalf("anthropic resources=%#v error=%v", resources, err)
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func TestNewChatResourcesBuildsOpenAIConversationWithoutNetwork(t *testing.T) {
	for _, baseURL := range []string{"https://example.com", "https://example.com/openai/v1"} {
		t.Run(baseURL, func(t *testing.T) {
			resources, err := newChatResources(config.Config{Provider: config.Provider{
				Family:  domain.ProviderOpenAI,
				BaseURL: baseURL,
				APIKey:  secret.New("test-key"),
				Model:   "gpt-test",
			}})
			if err != nil {
				t.Fatalf("new resources: %v", err)
			}
			shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := resources.close(shutdownContext); err != nil {
				t.Fatalf("close resources: %v", err)
			}
		})
	}
}

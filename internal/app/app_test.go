package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestRunVersionDoesNotReadConfiguration(t *testing.T) {
	var output bytes.Buffer
	err := Run(context.Background(), Options{
		ShowVersion: true,
		ConfigPath:  filepath.Join(t.TempDir(), "missing.json"),
		Output:      &output,
	})
	if err != nil {
		t.Fatalf("run version: %v", err)
	}
	if !strings.Contains(output.String(), Version) {
		t.Fatalf("version output: %q", output.String())
	}
}

func TestRunRejectsMissingExplicitConfiguration(t *testing.T) {
	var output bytes.Buffer
	missingPath := filepath.Join(t.TempDir(), "private-path-secret", "missing.json")
	err := Run(context.Background(), Options{
		ConfigPath: missingPath,
		Output:     &output,
	})
	if !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
		t.Fatalf("missing configuration error: %v", err)
	}
	if strings.Contains(err.Error(), missingPath) || strings.Contains(err.Error(), "private-path-secret") {
		t.Fatalf("configuration error leaked path: %v", err)
	}
}

func TestRunRejectsInvalidConfigurationWithoutLeakingSecret(t *testing.T) {
	clearProviderEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"provider":"openai","base_url":"https://example.com/v1","api_key":"top-secret"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	var output bytes.Buffer
	err := Run(context.Background(), Options{ConfigPath: path, Output: &output})
	if !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
		t.Fatalf("invalid configuration error: %v", err)
	}
	if strings.Contains(err.Error(), "top-secret") || strings.Contains(output.String(), "top-secret") {
		t.Fatalf("configuration error leaked API key: %v", err)
	}
}

func TestNewChatResourcesRejectsMissingConfiguration(t *testing.T) {
	_, err := newChatResources(config.Config{})
	if !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
		t.Fatalf("missing configuration error: %v", err)
	}
}

func clearProviderEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
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

func TestLoadedConfigurationStaysTypedAcrossAppBoundary(t *testing.T) {
	clearProviderEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"provider":"openai","base_url":"https://example.com/openai/v1","api_key":"boundary-secret","model":"gpt-test"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	resources, err := newChatResources(loaded)
	if err != nil {
		t.Fatalf("new chat resources: %v", err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := resources.close(shutdownContext); err != nil {
		t.Fatalf("close resources: %v", err)
	}
	if strings.Contains(loaded.String(), "boundary-secret") || strings.Contains(loaded.String(), path) {
		t.Fatalf("typed configuration leaked sensitive input: %s", loaded)
	}
}

func TestRunStartsTUIFromSupportedConfigurationSourcesWithoutNetwork(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		configure func(*testing.T, string) Options
	}{
		{
			name:    "explicit host-only file",
			baseURL: "https://example.com",
			configure: func(t *testing.T, baseURL string) Options {
				path := writeAppConfig(t, t.TempDir(), baseURL)
				return Options{ConfigPath: path}
			},
		},
		{
			name:    "explicit path-prefix file",
			baseURL: "https://example.com/openai/v1",
			configure: func(t *testing.T, baseURL string) Options {
				path := writeAppConfig(t, t.TempDir(), baseURL)
				return Options{ConfigPath: path}
			},
		},
		{
			name:    "default file",
			baseURL: "https://example.com/v1",
			configure: func(t *testing.T, baseURL string) Options {
				home := t.TempDir()
				t.Setenv("HOME", home)
				writeAppConfigAt(t, filepath.Join(home, ".config", "easycode", "config.json"), baseURL)
				return Options{}
			},
		},
		{
			name:    "environment only",
			baseURL: "https://example.com/v1",
			configure: func(t *testing.T, baseURL string) Options {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("EASYCODE_PROVIDER", "openai")
				t.Setenv("EASYCODE_BASE_URL", baseURL)
				t.Setenv("EASYCODE_API_KEY", "environment-secret")
				t.Setenv("EASYCODE_MODEL", "gpt-test")
				return Options{}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearProviderEnvironment(t)
			options := test.configure(t, test.baseURL)
			var output bytes.Buffer
			options.Input = strings.NewReader("\x03")
			options.Output = &output
			if err := Run(context.Background(), options); err != nil {
				t.Fatalf("run TUI: %v", err)
			}
			if !strings.Contains(output.String(), "Status: idle") {
				t.Fatalf("TUI did not enter idle state: %q", output.String())
			}
			if strings.Contains(output.String(), "file-secret") || strings.Contains(output.String(), "environment-secret") {
				t.Fatalf("TUI output leaked API key: %q", output.String())
			}
		})
	}
}

func TestRunCanRecoverAfterConfigurationError(t *testing.T) {
	clearProviderEnvironment(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(`{"provider":"openai"}`), 0o600); err != nil {
		t.Fatalf("write invalid configuration: %v", err)
	}
	var output bytes.Buffer
	if err := Run(context.Background(), Options{ConfigPath: path, Output: &output}); !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
		t.Fatalf("invalid configuration error: %v", err)
	}

	writeAppConfigAt(t, path, "https://example.com/v1")
	output.Reset()
	if err := Run(context.Background(), Options{
		ConfigPath: path,
		Input:      strings.NewReader("\x03"),
		Output:     &output,
	}); err != nil {
		t.Fatalf("run after configuration repair: %v", err)
	}
}

func writeAppConfig(t *testing.T, directory string, baseURL string) string {
	t.Helper()
	path := filepath.Join(directory, "config.json")
	writeAppConfigAt(t, path, baseURL)
	return path
}

func writeAppConfigAt(t *testing.T, path string, baseURL string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create configuration directory: %v", err)
	}
	content := `{"provider":"openai","base_url":"` + baseURL + `","api_key":"file-secret","model":"gpt-test"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod configuration: %v", err)
	}
}

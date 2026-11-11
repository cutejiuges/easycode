package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
)

func withReasoningConfig(t *testing.T, path, fields string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimSpace(string(data)), "}") + "," + fields + "}"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
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

func TestInvalidReasoningConfigurationBeforeAnyResources(t *testing.T) {
	clearProviderEnvironment(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	for _, tc := range []struct{ family, fields string }{
		{"anthropic", `"anthropic_thinking":{"type":"disabled","display":"omitted"}`},
		{"anthropic", `"anthropic_thinking":{"type":"adaptive","display":"updates"}`},
		{"anthropic", `"anthropic_thinking":{"type":"enabled","budget_tokens":4096}`},
		{"openai", `"anthropic_thinking":{"type":"disabled"}`},
		{"openai", `"openai_reasoning_summary":"auto","context_window_tokens":8192`},
		{"openai", `"max_output_tokens":65537`},
	} {
		root := filepath.Join(t.TempDir(), "sessions")
		path := filepath.Join(t.TempDir(), "config.json")
		body := fmt.Sprintf(`{"provider":%q,"base_url":%q,"api_key":"reasoning-private-secret","model":"fixture-model",%s}`, tc.family, server.URL, tc.fields)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		result := Run(context.Background(), Options{Mode: headless.ModeText, Prompt: "work", ConfigPath: path, SessionDataRoot: root, Output: &output})
		if result.Failure.Code != fault.CodeInvalidConfiguration || requests.Load() != 0 {
			t.Fatal("configuration admitted resources", result)
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("configuration created session", err)
		}
		if strings.Contains(output.String()+result.Failure.Message, "reasoning-private-secret") {
			t.Fatal("configuration leaked secret")
		}
	}
}

func TestReasoningStartupValueSurvivesConfigurationFileEdits(t *testing.T) {
	clearProviderEnvironment(t)
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				requests = append(requests, body)
				n := len(requests)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				if family == domain.ProviderOpenAI {
					writeOpenAIAppTurn(w, n)
				} else {
					writeAnthropicAppTurn(w, n)
				}
			}))
			defer server.Close()
			path := withReasoningConfig(t, writeAppConfig(t, t.TempDir(), family, server.URL), reasoningFields(family))
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			resources, err := newTestChatResources(t, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer resources.close(context.Background())
			if resources.inference.Family() != family {
				t.Fatal("startup view missing")
			}
			submitAppTurn(t, resources, "first")
			if err := os.WriteFile(path, []byte(`{"anthropic_thinking":null}`), 0600); err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, resources, "second")
			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
			if len(captured) != 2 {
				t.Fatal("unexpected requests", len(captured))
			}
			for _, body := range captured {
				assertReasoningRequest(t, family, body, false)
			}
			if resources.provider.Capabilities().ReasoningSummary != (family == domain.ProviderOpenAI) {
				t.Fatal("app capability inconsistent")
			}
		})
	}
}

func TestReasoningResumeUsesNewStartupSettingsWithoutRewritingHistory(t *testing.T) {
	clearProviderEnvironment(t)
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			var calls atomic.Int32
			var mu sync.Mutex
			var last []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				last = body
				mu.Unlock()
				n := int(calls.Add(1))
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write(activeReasoningFixture(family, n, false))
			}))
			defer server.Close()
			path := withReasoningConfig(t, writeAppConfig(t, t.TempDir(), family, server.URL), reasoningFields(family))
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			root, cwd := filepath.Join(t.TempDir(), "sessions"), t.TempDir()
			live, err := openChatResources(context.Background(), cfg, root, "", cwd)
			if err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, live, "first")
			thread := live.identity.ThreadID
			journal, _ := live.service.repository.JournalPath(thread)
			if err := live.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(journal)
			changed := `"openai_reasoning_effort":"low","openai_reasoning_summary":"detailed","max_output_tokens":8192`
			if family == domain.ProviderAnthropic {
				changed = `"anthropic_thinking":{"type":"adaptive","display":"summarized"},"anthropic_effort":"low","max_output_tokens":8192`
			}
			path = withReasoningConfig(t, writeAppConfig(t, t.TempDir(), family, server.URL), changed)
			cfg, err = config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := openChatResources(context.Background(), cfg, root, string(thread), cwd)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.close(context.Background())
			after, _ := os.ReadFile(journal)
			if !bytes.Equal(before, after) || calls.Load() != 1 {
				t.Fatal("resume rewrote history or sampled implicitly")
			}
			submitAppTurn(t, restored, "next explicitly")
			mu.Lock()
			body := append([]byte(nil), last...)
			mu.Unlock()
			if !bytes.Contains(body, []byte(`"effort":"low"`)) || !bytes.Contains(body, []byte("opaque-reasoning-sentinel")) || calls.Load() != 2 {
				t.Fatal("new selection or native history lost")
			}
			after, _ = os.ReadFile(journal)
			if !bytes.HasPrefix(after, before) {
				t.Fatal("append-only prefix changed")
			}
		})
	}
}

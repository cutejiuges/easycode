package config

import (
	"fmt"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func inferenceJSON(family, fields string) string {
	return fmt.Sprintf(`{"provider":%q,"model":"fixture-model","api_key":"reasoning-secret","base_url":"https://example.invalid"%s}`, family, fields)
}

func TestReasoningJSONBranchesBoundariesAndWindow(t *testing.T) {
	clearEnvironment(t)
	for _, tc := range []struct {
		family, fields string
		valid          bool
	}{
		{"anthropic", ``, true}, {"openai", ``, true},
		{"anthropic", `,"anthropic_thinking":{"type":"disabled"}`, true},
		{"anthropic", `,"anthropic_thinking":{"type":"adaptive","display":"summarized"},"anthropic_effort":"high"`, true},
		{"anthropic", `,"anthropic_thinking":{"type":"enabled","budget_tokens":8192,"display":"omitted"},"max_output_tokens":16384`, true},
		{"anthropic", `,"anthropic_thinking":{"type":"enabled","budget_tokens":4095}`, true},
		{"anthropic", `,"anthropic_thinking":{"type":"enabled","budget_tokens":1024}`, true},
		{"anthropic", `,"anthropic_thinking":{"type":"enabled","budget_tokens":1023}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"enabled","budget_tokens":4096}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"adaptive","budget_tokens":0}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"enabled"}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"disabled","display":"omitted"}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"adaptive","display":"updates"}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"adaptive","display":null}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"adaptive","display":"Summarized"}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"adaptive","display":"omitted","display":"omitted"}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"adaptive","Display":"omitted"}`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"adaptive","future":true}`, false},
		{"anthropic", `,"anthropic_thinking":null`, false},
		{"anthropic", `,"anthropic_thinking":{"type":"disabled"},"anthropic_effort":"max"`, false},
		{"openai", `,"anthropic_thinking":{"type":"disabled"}`, false},
		{"anthropic", `,"openai_reasoning_effort":"none"`, false},
		{"openai", `,"openai_reasoning_summary":"auto"`, true},
		{"openai", `,"openai_reasoning_summary":""`, false},
		{"openai", `,"openai_reasoning_effort":"HIGH"`, false},
		{"openai", `,"openai_reasoning_summary":null`, false},
		{"openai", `,"max_output_tokens":0`, false}, {"openai", `,"max_output_tokens":1`, true},
		{"openai", `,"max_output_tokens":65536`, true}, {"openai", `,"max_output_tokens":65537`, false},
		{"openai", `,"max_output_tokens":18446744073709551616`, false},
		{"openai", `,"max_output_tokens":1.0`, false}, {"openai", `,"max_output_tokens":1e3`, false},
		{"openai", `,"max_output_tokens":null`, false}, {"openai", `,"max_output_tokens":-1`, false},
		{"anthropic", `,"context_window_tokens":32`, true},
		{"anthropic", `,"context_window_tokens":8192,"anthropic_effort":"low"`, false},
		{"anthropic", `,"context_window_tokens":8192,"reserved_output_tokens":4096,"anthropic_effort":"low"`, true},
		{"openai", `,"context_window_tokens":8192,"reserved_output_tokens":4096,"openai_reasoning_summary":"auto"`, false},
		{"openai", `,"context_window_tokens":8192,"reserved_output_tokens":4096,"max_output_tokens":4096,"openai_reasoning_summary":"auto"`, true},
		{"openai", `,"context_window_tokens":32768,"reserved_output_tokens":8192,"max_output_tokens":16384`, false},
	} {
		t.Run(tc.family+tc.fields, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, t.TempDir(), inferenceJSON(tc.family, tc.fields), 0600))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
			if err != nil && strings.Contains(err.Error(), "reasoning-secret") {
				t.Fatal("secret leaked")
			}
			if err == nil {
				s, e := cfg.InferenceSettings()
				if e != nil || s.Family() != domain.ProviderFamily(tc.family) {
					t.Fatal(s, e)
				}
			}
		})
	}
}

func TestReasoningEnumsAndEnvironmentFamilyOverride(t *testing.T) {
	clearEnvironment(t)
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		if _, err := Load(writeConfig(t, t.TempDir(), inferenceJSON("anthropic", `,"anthropic_effort":"`+effort+`"`), 0600)); err != nil {
			t.Fatal(err)
		}
	}
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		for _, summary := range []string{"auto", "concise", "detailed"} {
			if _, err := Load(writeConfig(t, t.TempDir(), inferenceJSON("openai", `,"openai_reasoning_effort":"`+effort+`","openai_reasoning_summary":"`+summary+`"`), 0600)); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("EASYCODE_PROVIDER", "openai")
	if _, err := Load(writeConfig(t, t.TempDir(), inferenceJSON("anthropic", `,"anthropic_thinking":{"type":"disabled"}`), 0600)); err == nil {
		t.Fatal("family override bypassed validation")
	}
}

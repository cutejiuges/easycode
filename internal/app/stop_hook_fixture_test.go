package app

import (
	"easycode/internal/codec"
	"easycode/internal/domain"
	"os"
	"path/filepath"
	"testing"
)

func writeStopHookConfig(t *testing.T, workspace, command string, policy domain.HookFeedbackPolicy) {
	t.Helper()
	type declaration struct {
		Type     string                    `json:"type"`
		ID       string                    `json:"id"`
		Command  string                    `json:"command"`
		Feedback domain.HookFeedbackPolicy `json:"feedback"`
	}
	value := struct {
		Hooks struct {
			Stop []struct {
				Hooks []declaration `json:"hooks"`
			} `json:"Stop"`
		} `json:"hooks"`
	}{}
	value.Hooks.Stop = append(value.Hooks.Stop, struct {
		Hooks []declaration `json:"hooks"`
	}{[]declaration{{"command", "completion", command, policy}}})
	b, err := codec.MarshalStable(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".easycode"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".easycode", "hooks.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

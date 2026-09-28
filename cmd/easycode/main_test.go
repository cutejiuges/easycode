package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelpShowsConfigDefault(t *testing.T) {
	var output bytes.Buffer
	exitCode := run(context.Background(), []string{"--help"}, strings.NewReader(""), &output, &output)
	if exitCode != 0 {
		t.Fatalf("help exit code: %d output=%q", exitCode, output.String())
	}
	if !strings.Contains(output.String(), "--config") || !strings.Contains(output.String(), "~/.config/easycode/config.json") ||
		!strings.Contains(output.String(), "--resume") || strings.Contains(output.String(), "--continue") {
		t.Fatalf("help output: %q", output.String())
	}
}

func TestRunResumeRequiresValue(t *testing.T) {
	var output bytes.Buffer
	exitCode := run(context.Background(), []string{"--resume"}, strings.NewReader(""), &output, &output)
	if exitCode != 2 || !strings.Contains(output.String(), "flag needs an argument") {
		t.Fatalf("resume missing value exit=%d output=%q", exitCode, output.String())
	}
}

func TestRunInvalidResumeDoesNotCreateReplacementJournal(t *testing.T) {
	clearCLIEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("EASYCODE_PROVIDER", "openai")
	t.Setenv("EASYCODE_BASE_URL", "https://example.invalid/v1")
	t.Setenv("EASYCODE_API_KEY", "resume-secret")
	t.Setenv("EASYCODE_MODEL", "gpt-test")
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(), []string{"--resume", "not-a-thread-id"}, strings.NewReader("\x03"),
		&output, &errorOutput,
	)
	if exitCode != 1 || !strings.Contains(errorOutput.String(), "session_not_found") {
		t.Fatalf("invalid resume exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
	if strings.Contains(errorOutput.String(), "resume-secret") {
		t.Fatalf("invalid resume leaked secret: %q", errorOutput.String())
	}
	root := filepath.Join(home, ".easycode", "sessions")
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry != nil && !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			t.Fatalf("invalid resume created replacement journal %q", path)
		}
		return nil
	})
}

func TestRunVersionIgnoresMissingExplicitConfig(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"--version", "--config", filepath.Join(t.TempDir(), "missing.json")},
		strings.NewReader(""),
		&output,
		&errorOutput,
	)
	if exitCode != 0 || !strings.Contains(output.String(), "easycode") || errorOutput.Len() != 0 {
		t.Fatalf("version exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
}

func TestRunExplicitMissingConfigFails(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"--config", filepath.Join(t.TempDir(), "missing.json")},
		strings.NewReader(""),
		&output,
		&errorOutput,
	)
	if exitCode != 1 || !strings.Contains(errorOutput.String(), "invalid_configuration") {
		t.Fatalf("missing config exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
}

func TestRunExplicitInvalidConfigDoesNotLeakSecret(t *testing.T) {
	clearCLIEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"provider":"openai","base_url":"https://example.com/v1","api_key":"cli-secret"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"--config", path},
		strings.NewReader(""),
		&output,
		&errorOutput,
	)
	if exitCode != 1 || !strings.Contains(errorOutput.String(), "invalid_configuration") {
		t.Fatalf("invalid config exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
	if strings.Contains(errorOutput.String(), "cli-secret") {
		t.Fatalf("CLI error leaked API key: %q", errorOutput.String())
	}
}

func clearCLIEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
	}
}

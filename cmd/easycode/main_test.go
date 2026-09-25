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
	if !strings.Contains(output.String(), "--config") || !strings.Contains(output.String(), "~/.config/easycode/config.json") {
		t.Fatalf("help output: %q", output.String())
	}
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

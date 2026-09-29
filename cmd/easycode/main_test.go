package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/codec"
)

const cliHeadlessHelperEnv = "EASYCODE_TEST_CLI_HEADLESS_HELPER"

func TestRunHelpShowsConfigDefault(t *testing.T) {
	var output bytes.Buffer
	exitCode := run(context.Background(), []string{"--help"}, strings.NewReader(""), &output, &output, false)
	if exitCode != 0 {
		t.Fatalf("help exit code: %d output=%q", exitCode, output.String())
	}
	if !strings.Contains(output.String(), "--config") || !strings.Contains(output.String(), "~/.config/easycode/config.json") ||
		!strings.Contains(output.String(), "--resume") || !strings.Contains(output.String(), "-json") ||
		strings.Contains(output.String(), "--continue") {
		t.Fatalf("help output: %q", output.String())
	}
}

func TestRunRejectsConflictingHeadlessModesBeforeConfiguration(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(), []string{"--print", "--json", "hello"}, strings.NewReader(""),
		&output, &errorOutput, true,
	)
	if exitCode != 2 || output.Len() != 0 || !strings.Contains(errorOutput.String(), "mutually exclusive") {
		t.Fatalf("conflict exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
}

func TestRunRejectsMissingTerminalPromptWithoutCreatingSession(t *testing.T) {
	clearCLIEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(), []string{"--print"}, strings.NewReader(""),
		&output, &errorOutput, true,
	)
	if exitCode != 2 || output.Len() != 0 || !strings.Contains(errorOutput.String(), "prompt is required") {
		t.Fatalf("missing prompt exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".easycode", "sessions")); !os.IsNotExist(err) {
		t.Fatalf("invalid input created session root: %v", err)
	}
}

func TestRunResumeRequiresValue(t *testing.T) {
	var output bytes.Buffer
	exitCode := run(context.Background(), []string{"--resume"}, strings.NewReader(""), &output, &output, false)
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
		&output, &errorOutput, false,
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

func TestRunJSONInvalidResumeEmitsErrorWithoutReplacementJournal(t *testing.T) {
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
		context.Background(), []string{"--json", "--resume", "not-a-thread-id", "hello"},
		strings.NewReader(""), &output, &errorOutput, true,
	)
	if exitCode != 1 || errorOutput.Len() != 0 ||
		!strings.Contains(output.String(), `"type":"error"`) ||
		!strings.Contains(output.String(), `"code":"session_not_found"`) {
		t.Fatalf("invalid JSON resume exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
	if strings.Contains(output.String(), "resume-secret") {
		t.Fatalf("invalid JSON resume leaked secret: %q", output.String())
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
		false,
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
		false,
	)
	if exitCode != 1 || !strings.Contains(errorOutput.String(), "invalid_configuration") {
		t.Fatalf("missing config exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
}

func TestRunJSONStartupErrorIsSingleMachineEvent(t *testing.T) {
	missingPath := filepath.Join(t.TempDir(), "private-path-secret", "missing.json")
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(), []string{"--json", "--config", missingPath, "hello"},
		strings.NewReader(""), &output, &errorOutput, true,
	)
	if exitCode != 1 || errorOutput.Len() != 0 || strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("JSON startup exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
	var event struct {
		Version int    `json:"version"`
		Type    string `json:"type"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := codec.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event.Version != 1 || event.Type != "error" || event.Error.Code != "invalid_configuration" {
		t.Fatalf("startup event = %#v", event)
	}
	if strings.Contains(output.String(), missingPath) || strings.Contains(output.String(), "private-path-secret") {
		t.Fatalf("startup event leaked path: %s", output.String())
	}
}

func TestRunTextStartupErrorUsesOnlyStderr(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(),
		[]string{"--print", "--config", filepath.Join(t.TempDir(), "missing.json"), "hello"},
		strings.NewReader(""), &output, &errorOutput, true,
	)
	if exitCode != 1 || output.Len() != 0 || !strings.Contains(errorOutput.String(), "invalid_configuration") {
		t.Fatalf("text startup exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
}

func TestHeadlessProcessExitCodesAndJSONStdout(t *testing.T) {
	successServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg-1\",\"role\":\"assistant\",\"phase\":\"final\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\"}}\n\n")
	}))
	defer successServer.Close()
	failureServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "private provider body", http.StatusBadGateway)
	}))
	defer failureServer.Close()

	tests := []struct {
		name       string
		mode       string
		configPath string
		wantExit   int
		wantType   string
		wantStderr string
	}{
		{name: "success", mode: "success", configPath: writeCLIConfig(t, successServer.URL), wantType: `"type":"turn.completed"`},
		{name: "runtime failure", mode: "failure", configPath: writeCLIConfig(t, failureServer.URL), wantExit: 1, wantType: `"type":"turn.failed"`},
		{name: "usage failure", mode: "usage", wantExit: 2, wantStderr: "mutually exclusive"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestCLIHeadlessHelperProcess$")
			home := t.TempDir()
			command.Env = append(os.Environ(),
				"HOME="+home,
				"EASYCODE_PROVIDER=", "EASYCODE_BASE_URL=", "EASYCODE_API_KEY=", "EASYCODE_MODEL=",
				cliHeadlessHelperEnv+"=1",
				"EASYCODE_TEST_CLI_HEADLESS_MODE="+test.mode,
				"EASYCODE_TEST_CLI_HEADLESS_CONFIG="+test.configPath,
			)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			err := command.Run()
			exitCode := 0
			if err != nil {
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) {
					t.Fatal(err)
				}
				exitCode = exitError.ExitCode()
			}
			if exitCode != test.wantExit {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}
			if test.wantType != "" {
				if !strings.Contains(stdout.String(), test.wantType) || stderr.Len() != 0 {
					t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
				}
				for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
					if !codec.Valid([]byte(line)) {
						t.Fatalf("invalid JSONL line: %q", line)
					}
				}
			}
			if test.wantStderr != "" && (stdout.Len() != 0 || !strings.Contains(stderr.String(), test.wantStderr)) {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestCLIHeadlessHelperProcess(t *testing.T) {
	if os.Getenv(cliHeadlessHelperEnv) != "1" {
		return
	}
	mode := os.Getenv("EASYCODE_TEST_CLI_HEADLESS_MODE")
	arguments := []string{"--json", "--config", os.Getenv("EASYCODE_TEST_CLI_HEADLESS_CONFIG"), "hello"}
	if mode == "usage" {
		arguments = []string{"--print", "--json", "hello"}
	}
	exitCode := run(context.Background(), arguments, strings.NewReader(""), os.Stdout, os.Stderr, true)
	os.Exit(exitCode)
}

func writeCLIConfig(t *testing.T, baseURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"provider":"openai","base_url":"` + baseURL + `","api_key":"process-secret","model":"gpt-test"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
		false,
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

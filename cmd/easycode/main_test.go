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

func TestRunRejectsConflictingSessionSelectorsBeforeConfigurationAndPrompt(t *testing.T) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	exitCode := run(
		context.Background(), []string{"--print", "--resume", "thread", "--continue"},
		strings.NewReader("prompt-that-must-not-be-read"), &output, &errorOutput, false,
	)
	if exitCode != 2 || output.Len() != 0 || !strings.Contains(errorOutput.String(), "mutually exclusive") {
		t.Fatalf("conflict exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
	}
}

func TestRunValidatesStreamJSONMatrixBeforeApplicationSideEffects(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		terminal  bool
	}{
		{name: "requires json", arguments: []string{"--input-format", "stream-json"}},
		{name: "rejects print", arguments: []string{"--print", "--input-format", "stream-json"}},
		{name: "rejects prompt", arguments: []string{"--json", "--input-format", "stream-json", "hello"}},
		{name: "rejects dash", arguments: []string{"--json", "--input-format", "stream-json", "-"}},
		{name: "rejects terminal", arguments: []string{"--json", "--input-format", "stream-json"}, terminal: true},
		{name: "rejects unknown format", arguments: []string{"--json", "--input-format", "future"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			var output bytes.Buffer
			var errorOutput bytes.Buffer
			exitCode := run(
				context.Background(), test.arguments, strings.NewReader("must-not-be-read"),
				&output, &errorOutput, test.terminal,
			)
			if exitCode != 2 || output.Len() != 0 || errorOutput.Len() == 0 {
				t.Fatalf("exit=%d output=%q error=%q", exitCode, output.String(), errorOutput.String())
			}
			if _, err := os.Stat(filepath.Join(home, ".easycode", "sessions")); !os.IsNotExist(err) {
				t.Fatalf("invalid matrix created session state: %v", err)
			}
		})
	}
}

func TestHeadlessProcessExitCodesAndJSONStdout(t *testing.T) {
	successServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-1\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg-1\",\"role\":\"assistant\",\"phase\":\"final\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"usage\":{\"input_tokens\":10,\"input_tokens_details\":{\"cached_tokens\":2},\"cache_write_tokens\":0,\"output_tokens\":5,\"output_tokens_details\":{\"reasoning_tokens\":0}}}}\n\n")
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
		{name: "stream success", mode: "stream-success", configPath: writeCLIConfig(t, successServer.URL), wantType: `"type":"turn.completed"`},
		{name: "stream turn failure is reported", mode: "stream-failure", configPath: writeCLIConfig(t, failureServer.URL), wantType: `"type":"turn.failed"`},
		{name: "stream malformed input", mode: "stream-malformed", configPath: writeCLIConfig(t, successServer.URL), wantExit: 1, wantType: `"type":"error"`},
		{name: "stream explicit shutdown", mode: "stream-shutdown", configPath: writeCLIConfig(t, successServer.URL), wantType: `"type":"control.response"`},
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
			switch test.mode {
			case "stream-success", "stream-failure":
				command.Stdin = strings.NewReader(`{"version":1,"type":"input.submit","request_id":"process-input","text":"hello"}` + "\n")
			case "stream-malformed":
				command.Stdin = strings.NewReader(`{"version":1,"type":"input.submit","request_id":"process-input","text":"private","unknown":true}` + "\n")
			case "stream-shutdown":
				command.Stdin = strings.NewReader(`{"version":1,"type":"session.shutdown","request_id":"process-shutdown"}`)
			}
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
				if test.mode == "success" {
					assertCLICompletionUsage(t, stdout.String())
				}
			}
			if test.wantStderr != "" && (stdout.Len() != 0 || !strings.Contains(stderr.String(), test.wantStderr)) {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func assertCLICompletionUsage(t *testing.T, output string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var event struct {
			Type  string `json:"type"`
			Usage struct {
				InputUncached struct {
					State string  `json:"state"`
					Value *uint64 `json:"value,omitempty"`
				} `json:"input_uncached"`
				CacheWrite struct {
					State string  `json:"state"`
					Value *uint64 `json:"value,omitempty"`
				} `json:"cache_write"`
				ReasoningOutput struct {
					State string  `json:"state"`
					Value *uint64 `json:"value,omitempty"`
				} `json:"reasoning_output"`
			} `json:"usage"`
		}
		if err := codec.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "turn.completed" {
			continue
		}
		if event.Usage.InputUncached.State != "known" || event.Usage.InputUncached.Value == nil ||
			*event.Usage.InputUncached.Value != 8 || event.Usage.CacheWrite.State != "known" ||
			event.Usage.CacheWrite.Value == nil || *event.Usage.CacheWrite.Value != 0 ||
			event.Usage.ReasoningOutput.State != "known" || event.Usage.ReasoningOutput.Value == nil ||
			*event.Usage.ReasoningOutput.Value != 0 {
			t.Fatalf("turn.completed usage = %#v", event.Usage)
		}
		return
	}
	t.Fatal("turn.completed event is missing")
}

func TestCLIHeadlessHelperProcess(t *testing.T) {
	if os.Getenv(cliHeadlessHelperEnv) != "1" {
		return
	}
	mode := os.Getenv("EASYCODE_TEST_CLI_HEADLESS_MODE")
	arguments := []string{"--json", "--config", os.Getenv("EASYCODE_TEST_CLI_HEADLESS_CONFIG"), "hello"}
	input := io.Reader(strings.NewReader(""))
	stdinIsTerminal := true
	if mode == "usage" {
		arguments = []string{"--print", "--json", "hello"}
	} else if strings.HasPrefix(mode, "stream-") {
		arguments = []string{"--json", "--input-format", "stream-json", "--config", os.Getenv("EASYCODE_TEST_CLI_HEADLESS_CONFIG")}
		input = os.Stdin
		stdinIsTerminal = false
	}
	exitCode := run(context.Background(), arguments, input, os.Stdout, os.Stderr, stdinIsTerminal)
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

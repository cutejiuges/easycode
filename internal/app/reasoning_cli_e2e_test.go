//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/session"
)

type reasoningCLIResult struct {
	stdout, stderr string
	exit           int
}

func runReasoningCLI(t *testing.T, binary, home, workspace, input string, arguments ...string) reasoningCLIResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = workspace
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key == "HOME" || strings.HasPrefix(key, "EASYCODE_") {
			continue
		}
		command.Env = append(command.Env, value)
	}
	command.Env = append(command.Env, "HOME="+home)
	command.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatal("CLI failed to finish and release resources before its deadline")
	}
	if err != nil && command.ProcessState == nil {
		t.Fatalf("start CLI: %v", err)
	}
	return reasoningCLIResult{stdout.String(), stderr.String(), command.ProcessState.ExitCode()}
}

func reasoningCLIArguments(mode, configuration string, texts ...string) ([]string, string) {
	arguments := []string{"--config", configuration}
	if mode == "print" {
		return append(arguments, "--print", texts[0]), ""
	}
	arguments = append(arguments, "--json")
	if mode == "json" {
		return append(arguments, texts[0]), ""
	}
	arguments = append(arguments, "--input-format", "stream-json")
	var input strings.Builder
	for i, text := range texts {
		fmt.Fprintf(&input, "{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"cli-%d\",\"text\":%q}\n", i, text)
	}
	return arguments, input.String()
}

func assertReasoningCLIEvents(t *testing.T, result reasoningCLIResult, mode string, completed, failed int) {
	t.Helper()
	for _, private := range []string{"file-secret", "allowed-reasoning-sentinel", "opaque-reasoning-sentinel", "raw-reasoning-sentinel", "usage-private-sentinel"} {
		if strings.Contains(result.stdout+result.stderr, private) {
			t.Fatalf("CLI leaked private content %s", private)
		}
	}
	if mode == "print" {
		return
	}
	completions, failures := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(result.stdout), "\n") {
		if line == "" {
			continue
		}
		var event struct {
			Version int             `json:"version"`
			Type    string          `json:"type"`
			Usage   json.RawMessage `json:"usage"`
		}
		if codec.Unmarshal([]byte(line), &event) != nil || event.Version != 1 {
			t.Fatalf("CLI stdout is not JSONL v1: %q", line)
		}
		switch event.Type {
		case "turn.completed":
			completions++
			var usage headless.Usage
			if codec.Unmarshal(event.Usage, &usage) != nil {
				t.Fatal("CLI completed event lost usage")
			}
			wantThinking, wantOutput := uint64(60), uint64(80)
			if completions > 1 {
				wantThinking, wantOutput = 30, 40
			}
			if usage.Output.State != "known" || usage.ReasoningOutput.State != "known" || usage.Output.Value == nil || usage.ReasoningOutput.Value == nil || *usage.Output.Value != wantOutput || *usage.ReasoningOutput.Value != wantThinking {
				t.Fatalf("CLI turn usage does not match native samples: %s", event.Usage)
			}
		case "turn.failed":
			failures++
		}
	}
	if completions != completed || failures != failed {
		t.Fatalf("CLI terminal counts completed=%d failed=%d, want %d/%d", completions, failures, completed, failed)
	}
}

func TestReasoningControlsBuiltCLIEndToEnd(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(privateAppTempDir(t), "easycode")
	buildContext, cancelBuild := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildContext, "go", "build", "-o", binary, "./cmd/easycode")
	build.Dir = root
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	// TestMain 隔离了应用 HOME；构建只复用本机已有 Go 缓存，禁止为测试下载依赖。
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "HOME" && key != "GOPROXY" && key != "GOSUMDB" {
			build.Env = append(build.Env, value)
		}
	}
	build.Env = append(build.Env, "HOME="+account.HomeDir, "GOPROXY=off", "GOSUMDB=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		var continuation []byte
		for _, mode := range []string{"print", "json", "stream-json"} {
			for _, scenario := range []string{"healthy", "duplicate-usage", "parameter-rejected", "invalid-config"} {
				t.Run(fmt.Sprintf("%s/%s/%s", family, mode, scenario), func(t *testing.T) {
					home, workspace := privateAppTempDir(t), privateAppTempDir(t)
					if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("fixed CLI read content"), 0600); err != nil {
						t.Fatal(err)
					}
					var mu sync.Mutex
					var requests [][]byte
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, _ := io.ReadAll(r.Body)
						mu.Lock()
						requests = append(requests, body)
						n := len(requests)
						mu.Unlock()
						assertReasoningRequest(t, family, body, false)
						if scenario == "parameter-rejected" {
							http.Error(w, "usage-private-sentinel", http.StatusBadRequest)
							return
						}
						wire := activeReasoningFixture(family, n, true)
						if scenario == "duplicate-usage" {
							wire = duplicateFinalReasoningUsage(t, family, wire)
						}
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = w.Write(wire)
					}))
					defer server.Close()
					fields := reasoningFields(family)
					if scenario == "invalid-config" {
						fields = `"max_output_tokens":0`
					}
					configuration := withReasoningConfig(t, writeAppConfig(t, privateAppTempDir(t), family, server.URL), fields)
					texts := []string{"Read README.md then answer"}
					if mode == "stream-json" && scenario == "healthy" {
						texts = append(texts, "continue")
					}
					arguments, input := reasoningCLIArguments(mode, configuration, texts...)
					result := runReasoningCLI(t, binary, home, workspace, input, arguments...)
					mu.Lock()
					requestCount := len(requests)
					mu.Unlock()
					if scenario != "healthy" {
						wantExit, wantRequests := 1, 1
						if mode == "stream-json" && scenario != "invalid-config" {
							wantExit = 0
						}
						if scenario == "invalid-config" {
							wantRequests = 0
							if _, err := os.Stat(filepath.Join(home, ".easycode")); !os.IsNotExist(err) {
								t.Fatal("invalid configuration created application resources")
							}
						}
						if result.exit != wantExit || requestCount != wantRequests {
							t.Fatalf("CLI failure boundary changed: %+v requests=%d", result, requestCount)
						}
						failed := 1
						if scenario == "invalid-config" {
							failed = 0
						}
						assertReasoningCLIEvents(t, result, mode, 0, failed)
						return
					}
					if result.exit != 0 || (mode == "print" && strings.TrimSpace(result.stdout) != "answer-2") {
						t.Fatalf("CLI tool loop failed: %+v", result)
					}
					completed := 1
					if mode == "stream-json" {
						completed = 2
					}
					assertReasoningCLIEvents(t, result, mode, completed, 0)
					dataRoot := filepath.Join(home, ".easycode", "sessions")
					repository, err := session.OpenOrCreateRepository(dataRoot)
					if err != nil {
						t.Fatal(err)
					}
					locations, err := repository.EnumerateJournals(context.Background())
					if closeErr := repository.Close(); err != nil || closeErr != nil || len(locations) != 1 {
						t.Fatal("CLI did not persist exactly one thread")
					}
					thread := locations[0].ThreadID
					journal := filepath.Join(dataRoot, filepath.FromSlash(locations[0].RelativePath))
					before, err := os.ReadFile(journal)
					if err != nil {
						t.Fatal(err)
					}
					if mode != "stream-json" {
						// 独立新进程恢复，同一最终请求应与流式进程内第二轮等价。
						resume := runReasoningCLI(t, binary, home, workspace, "", "--config", configuration, "--resume", string(thread), "--print", "continue")
						if resume.exit != 0 || strings.TrimSpace(resume.stdout) != "answer-3" {
							t.Fatalf("CLI resume failed: %+v", resume)
						}
						assertReasoningCLIEvents(t, resume, "print", 0, 0)
					}
					after, err := os.ReadFile(journal)
					if err != nil || !bytes.HasPrefix(after, before) {
						t.Fatal("CLI resume changed existing journal bytes")
					}
					loaded := loadHeadlessJournal(t, dataRoot, thread)
					plan, err := session.NewReplayPlanner().Plan(loaded)
					if err != nil || len(plan.Tools) != 1 || !plan.Tools[0].HasResult || len(plan.SampleUsages) != 3 {
						t.Fatal("CLI lost native pairing or replayed the tool", err)
					}
					if err := validateSampleUsageEvidence(plan); err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					captured := append([][]byte(nil), requests...)
					mu.Unlock()
					if len(captured) != 3 {
						t.Fatal("CLI repeated generation")
					}
					if continuation != nil && !bytes.Equal(continuation, captured[2]) {
						t.Fatal("CLI restored and uninterrupted native requests differ")
					}
					continuation = append([]byte(nil), captured[2]...)
				})
			}
		}
	}
}

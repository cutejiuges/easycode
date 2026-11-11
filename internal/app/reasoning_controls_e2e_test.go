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

	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/session"
)

// activeReasoningFixture 复用工具循环事件，提供与可见正文独立的明确计量。
func activeReasoningFixture(family domain.ProviderFamily, sample int, tool bool) []byte {
	wire := reasoningFixtureStream(family, sample, tool)
	if family == domain.ProviderOpenAI {
		return bytes.ReplaceAll(wire, []byte(`"response":{"id":`), []byte(`"response":{"usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":0},"output_tokens":40,"output_tokens_details":{"reasoning_tokens":30}},"id":`))
	}
	stop := "end_turn"
	if tool && sample == 1 {
		stop = "tool_use"
	}
	delta := fmt.Sprintf("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q},\"usage\":{\"output_tokens\":40,\"output_tokens_details\":{\"thinking_tokens\":30}}}\n\n", stop)
	return bytes.Replace(wire, []byte(`data: {"type":"message_stop"}`), []byte(delta+`data: {"type":"message_stop"}`), 1)
}

func TestReasoningControlsHeadlessToolLoopAndUsage(t *testing.T) {
	clearProviderEnvironment(t)
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(fmt.Sprintf("%s/%d", family, mode), func(t *testing.T) {
				workspace := t.TempDir()
				t.Chdir(workspace)
				if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("read exactly once"), 0600); err != nil {
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
					w.Header().Set("Content-Type", "text/event-stream")
					wire := activeReasoningFixture(family, n, true)
					for start := 0; start < len(wire); {
						end := min(len(wire), start+1+start%31)
						_, _ = w.Write(wire[start:end])
						start = end
					}
				}))
				defer server.Close()
				root := filepath.Join(privateAppTempDir(t), "sessions")
				path := withReasoningConfig(t, writeAppConfig(t, t.TempDir(), family, server.URL), reasoningFields(family))
				output := &appWriteCloser{}
				opts := Options{Mode: mode, Prompt: "Read README.md then answer", ConfigPath: path, SessionDataRoot: root, Output: output}
				if mode == headless.ModeStreamJSON {
					opts.Prompt = ""
					opts.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"work\",\"text\":\"Read README.md then answer\"}\n"))
				}
				outcome := Run(context.Background(), opts)
				if outcome.ExitCode() != 0 || !strings.Contains(output.String(), "answer-2") {
					t.Fatal(outcome, output.String())
				}
				for _, private := range []string{"allowed-reasoning-sentinel", "opaque-reasoning-sentinel", "raw-reasoning-sentinel", "ignored-start-sentinel", "\"thinking_tokens\"", "\"budget_tokens\""} {
					if strings.Contains(output.String(), private) {
						t.Fatal("headless privacy breach", private)
					}
				}
				mu.Lock()
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
				if len(captured) != 2 {
					t.Fatal("tool or request repeated", len(captured))
				}
				for _, body := range captured {
					assertReasoningRequest(t, family, body, false)
				}
				if !bytes.Contains(captured[1], []byte("opaque-reasoning-sentinel")) {
					t.Fatal("opaque reasoning lost on continuation")
				}
				repo, err := session.OpenOrCreateRepository(root)
				if err != nil {
					t.Fatal(err)
				}
				defer repo.Close()
				threads, err := repo.EnumerateJournals(context.Background())
				if err != nil || len(threads) != 1 {
					t.Fatal(threads, err)
				}
				lease, err := repo.Open(context.Background(), threads[0].ThreadID)
				if err != nil {
					t.Fatal(err)
				}
				defer lease.Close()
				loaded, err := session.NewLoader().Load(context.Background(), lease)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := session.NewReplayPlanner().Plan(loaded)
				if err != nil {
					t.Fatal(err)
				}
				if len(plan.SampleUsages) != 2 {
					t.Fatal("missing durable usage")
				}
				for _, sample := range plan.SampleUsages {
					v, k := sample.Usage.ReasoningOutput().Value()
					total, _ := sample.Usage.Output().Value()
					if !k || v != 30 || total != 40 {
						t.Fatal("incorrect durable usage", sample.Usage)
					}
				}
				if err := validateSampleUsageEvidence(plan); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestReasoningParameterFailuresNeverRetryOrDowngrade(t *testing.T) {
	clearProviderEnvironment(t)
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, status := range []int{400, 401, 429} {
			t.Run(fmt.Sprintf("%s/%d", family, status), func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, _ := io.ReadAll(r.Body)
					assertReasoningRequest(t, family, body, false)
					http.Error(w, "raw-private-response", status)
				}))
				defer server.Close()
				path := withReasoningConfig(t, writeAppConfig(t, t.TempDir(), family, server.URL), reasoningFields(family))
				var output bytes.Buffer
				outcome := Run(context.Background(), Options{Mode: headless.ModeJSON, Prompt: "work", ConfigPath: path, SessionDataRoot: filepath.Join(privateAppTempDir(t), "sessions"), Output: &output})
				if outcome.Failure.Code == "" || calls.Load() != 1 {
					t.Fatal("failure retried or succeeded", outcome, calls.Load())
				}
				if strings.Contains(output.String(), "raw-private-response") {
					t.Fatal("response body leaked")
				}
			})
		}
	}
}

func TestReasoningOutputTerminalDoesNotChangePolicy(t *testing.T) {
	clearProviderEnvironment(t)
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				assertReasoningRequest(t, family, body, false)
				w.Header().Set("Content-Type", "text/event-stream")
				if family == domain.ProviderOpenAI {
					fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"incomplete\"}}\n\ndata: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"incomplete\",\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n")
				} else {
					_, _ = w.Write(bytes.ReplaceAll(activeReasoningFixture(family, 1, false), []byte(`"stop_reason":"end_turn"`), []byte(`"stop_reason":"max_tokens"`)))
				}
			}))
			defer server.Close()
			path := withReasoningConfig(t, writeAppConfig(t, t.TempDir(), family, server.URL), reasoningFields(family))
			var output bytes.Buffer
			result := Run(context.Background(), Options{Mode: headless.ModeJSON, Prompt: "work", ConfigPath: path, SessionDataRoot: filepath.Join(privateAppTempDir(t), "sessions"), Output: &output})
			if calls.Load() != 1 || (result.ExitCode() == 0) != (family == domain.ProviderAnthropic) {
				t.Fatal("terminal changed or retried", result, calls.Load())
			}
			if family == domain.ProviderOpenAI && strings.Contains(output.String(), `"type":"turn.completed"`) {
				t.Fatal("incomplete became final answer")
			}
		})
	}
}

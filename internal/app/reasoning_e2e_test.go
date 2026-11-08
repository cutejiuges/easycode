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
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/session"
)

// reasoningFixtureStream 生成本地服务端固定事件，不接触任何公网模型。
func reasoningFixtureStream(family domain.ProviderFamily, sample int, withTool bool) []byte {
	var stream bytes.Buffer
	if family == domain.ProviderOpenAI {
		if withTool {
			writeOpenAIToolLoopSample(&stream, sample)
		} else {
			writeOpenAIAppTurn(&stream, sample)
		}
		frames := `data: {"type":"response.output_item.added","output_index":3,"item":{"type":"reasoning","id":"reasoning-fixture","summary":[]}}

data: {"type":"response.reasoning_summary_part.added","output_index":3,"item_id":"reasoning-fixture","summary_index":0,"part":{"type":"summary_text","text":""}}

data: {"type":"response.reasoning_summary_text.delta","output_index":3,"item_id":"reasoning-fixture","summary_index":0,"delta":"# allowed-reasoning-sentinel 中文🙂\n\n\u0060\u0060\u0060go\nreason()\n\u0060\u0060\u0060\n"}

data: {"type":"response.reasoning_summary_text.done","output_index":3,"item_id":"reasoning-fixture","summary_index":0,"text":"# allowed-reasoning-sentinel 中文🙂\n\n\u0060\u0060\u0060go\nreason()\n\u0060\u0060\u0060\n"}

data: {"type":"response.output_item.done","output_index":3,"item":{"type":"reasoning","id":"reasoning-fixture","summary":[{"type":"summary_text","text":"# allowed-reasoning-sentinel 中文🙂\n\n\u0060\u0060\u0060go\nreason()\n\u0060\u0060\u0060\n"}],"encrypted_content":"opaque-reasoning-sentinel","content":[{"type":"reasoning_text","text":"raw-reasoning-sentinel"}]}}

`
		return bytes.Replace(stream.Bytes(), []byte(`data: {"type":"response.completed"`), []byte(frames+`data: {"type":"response.completed"`), 1)
	}
	if withTool {
		writeAnthropicToolLoopSample(&stream, sample)
	} else {
		writeAnthropicAppTurn(&stream, sample)
	}
	frames := `data: {"type":"content_block_start","index":3,"content_block":{"type":"thinking","thinking":"ignored-start-sentinel","signature":"ignored-signature-sentinel"}}

data: {"type":"content_block_delta","index":3,"delta":{"type":"thinking_delta","thinking":"# allowed-reasoning-sentinel 中文🙂\n\n\u0060\u0060\u0060go\nreason()\n\u0060\u0060\u0060\n"}}

data: {"type":"content_block_delta","index":3,"delta":{"type":"signature_delta","signature":"opaque-reasoning-sentinel"}}

data: {"type":"content_block_stop","index":3}

`
	return bytes.Replace(stream.Bytes(), []byte(`data: {"type":"message_stop"`), []byte(frames+`data: {"type":"message_stop"`), 1)
}

func TestReasoningHeadlessBothProvidersAllModesWithToolAndEOF(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(fmt.Sprintf("%s/%d", family, mode), func(t *testing.T) {
				workspace := t.TempDir()
				t.Chdir(workspace)
				if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("local tool input\n"), 0600); err != nil {
					t.Fatal(err)
				}
				var mu sync.Mutex
				var requests [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					requests = append(requests, body)
					sample := len(requests)
					mu.Unlock()
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write(reasoningFixtureStream(family, sample, true))
				}))
				defer server.Close()
				output := &appWriteCloser{}
				options := Options{Mode: mode, Prompt: "Read README.md then answer", ConfigPath: writeAppConfig(t, t.TempDir(), family, server.URL), SessionDataRoot: filepath.Join(privateAppTempDir(t), "sessions"), Output: output}
				if mode == headless.ModeStreamJSON {
					options.Prompt = ""
					options.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"reasoning\",\"text\":\"Read README.md then answer\"}\n"))
				}
				outcome := Run(context.Background(), options)
				if outcome.ExitCode() != 0 || !strings.Contains(output.String(), "answer-2") {
					t.Fatalf("outcome=%#v output=%s", outcome, output.String())
				}
				for _, forbidden := range []string{"allowed-reasoning-sentinel", "opaque-reasoning-sentinel", "raw-reasoning-sentinel", "ignored-start-sentinel", "sample_index", "bindings", "availability"} {
					if strings.Contains(output.String(), forbidden) {
						t.Fatalf("stdout leaked %s", forbidden)
					}
				}
				mu.Lock()
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
				if len(captured) != 2 || !bytes.Contains(captured[1], []byte("allowed-reasoning-sentinel")) || !bytes.Contains(captured[1], []byte("opaque-reasoning-sentinel")) {
					t.Fatal("native continuation or tool lifecycle changed")
				}
				for _, request := range captured {
					for _, unrequested := range []string{"sequential_cutoff", `"effort"`, `"budget_tokens"`, `"thinking":{`, `"reasoning":{`} {
						if bytes.Contains(request, []byte(unrequested)) {
							t.Fatalf("passive display changed request policy: %s", unrequested)
						}
					}
				}
			})
		}
	}
}

func TestReasoningHeadlessCancelAndBrokenPipeMatrix(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			for _, broken := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/broken=%t", family, mode, broken), func(t *testing.T) {
					t.Chdir(t.TempDir())
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					started := make(chan struct{})
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "text/event-stream")
						wire := reasoningFixtureStream(family, 1, false)
						if broken {
							_, _ = w.Write(wire)
							return
						}
						marker := []byte(`data: {"type":"response.reasoning_summary_text.done"`)
						if family == domain.ProviderAnthropic {
							marker = []byte(`data: {"type":"content_block_delta","index":3,"delta":{"type":"signature_delta"`)
						}
						split := bytes.Index(wire, marker)
						if split < 0 {
							t.Error("missing fixture boundary")
							close(started)
							return
						}
						_, _ = w.Write(wire[:split])
						w.(http.Flusher).Flush()
						close(started)
						<-r.Context().Done()
					}))
					defer server.Close()
					root := filepath.Join(privateAppTempDir(t), "sessions")
					configPath := writeAppConfig(t, t.TempDir(), family, server.URL)
					output := &appWriteCloser{}
					options := Options{Mode: mode, Prompt: "work", ConfigPath: configPath, SessionDataRoot: root, Output: output}
					if mode == headless.ModeStreamJSON {
						options.Prompt = ""
						options.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"reasoning-cancel\",\"text\":\"work\"}\n"))
					}
					if broken {
						writer := &failOnHeadlessWrite{failCall: 3}
						if mode == headless.ModeText {
							writer.failCall = 1
						}
						options.Output = writer
						outcome := Run(ctx, options)
						if outcome.Failure.Code != fault.CodeOutput || outcome.ExitCode() != 1 {
							t.Fatal(outcome)
						}
					} else {
						done := make(chan Outcome, 1)
						go func() { done <- Run(ctx, options) }()
						<-started
						cancel()
						outcome := <-done
						if outcome.ExitCode() != 1 || outcome.Failure.Code != fault.CodeUserCancelled {
							t.Fatal(outcome)
						}
						for _, sentinel := range []string{"allowed-reasoning-sentinel", "opaque-reasoning-sentinel", "raw-reasoning-sentinel", "bindings", "sample_resolution"} {
							if strings.Contains(output.String(), sentinel) {
								t.Fatal("cancel output leaked reasoning", output.String())
							}
						}
					}
					repository, err := session.OpenOrCreateRepository(root)
					if err != nil {
						t.Fatal(err)
					}
					locations, err := repository.EnumerateJournals(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					if err := repository.Close(); err != nil {
						t.Fatal(err)
					}
					if len(locations) != 1 {
						t.Fatal("missing journal", len(locations))
					}
					loaded := loadHeadlessJournal(t, root, locations[0].ThreadID)
					if !broken {
						if loaded.Records[len(loaded.Records)-1].EventKind != session.EventTurnFailed {
							t.Fatal("cancel did not durably fail")
						}
						for _, record := range loaded.Records {
							if record.EventKind == session.EventSampleUsage {
								t.Fatal("unselected sample was committed")
							}
						}
					}
				})
			}
		}
	}
}

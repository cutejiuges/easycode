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
	"easycode/internal/headless"
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
	} else {
		if withTool {
			writeAnthropicToolLoopSample(&stream, sample)
		} else {
			writeAnthropicAppTurn(&stream, sample)
		}
	}
	return withReasoningFixture(family, stream.Bytes())
}

// withReasoningFixture 为已有工具或文本样本复用同一允许正文与 opaque 测试边界。
func withReasoningFixture(family domain.ProviderFamily, wire []byte) []byte {
	if family == domain.ProviderOpenAI {
		frames := `data: {"type":"response.output_item.added","output_index":3,"item":{"type":"reasoning","id":"reasoning-fixture","summary":[]}}

data: {"type":"response.reasoning_summary_part.added","output_index":3,"item_id":"reasoning-fixture","summary_index":0,"part":{"type":"summary_text","text":""}}

data: {"type":"response.reasoning_summary_text.delta","output_index":3,"item_id":"reasoning-fixture","summary_index":0,"delta":"# allowed-reasoning-sentinel 中文🙂\n\n\u0060\u0060\u0060go\nreason()\n\u0060\u0060\u0060\n"}

data: {"type":"response.reasoning_summary_text.done","output_index":3,"item_id":"reasoning-fixture","summary_index":0,"text":"# allowed-reasoning-sentinel 中文🙂\n\n\u0060\u0060\u0060go\nreason()\n\u0060\u0060\u0060\n"}

data: {"type":"response.output_item.done","output_index":3,"item":{"type":"reasoning","id":"reasoning-fixture","summary":[{"type":"summary_text","text":"# allowed-reasoning-sentinel 中文🙂\n\n\u0060\u0060\u0060go\nreason()\n\u0060\u0060\u0060\n"}],"encrypted_content":"opaque-reasoning-sentinel","content":[{"type":"reasoning_text","text":"raw-reasoning-sentinel"}]}}

`
		return bytes.Replace(wire, []byte(`data: {"type":"response.completed"`), []byte(frames+`data: {"type":"response.completed"`), 1)
	}
	frames := `data: {"type":"content_block_start","index":3,"content_block":{"type":"thinking","thinking":"ignored-start-sentinel","signature":"ignored-signature-sentinel"}}

data: {"type":"content_block_delta","index":3,"delta":{"type":"thinking_delta","thinking":"# allowed-reasoning-sentinel 中文🙂\n\n\u0060\u0060\u0060go\nreason()\n\u0060\u0060\u0060\n"}}

data: {"type":"content_block_delta","index":3,"delta":{"type":"signature_delta","signature":"opaque-reasoning-sentinel"}}

data: {"type":"content_block_stop","index":3}

`
	return bytes.Replace(wire, []byte(`data: {"type":"message_stop"`), []byte(frames+`data: {"type":"message_stop"`), 1)
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

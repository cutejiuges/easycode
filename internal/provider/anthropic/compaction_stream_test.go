package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestCompactionStreamFailuresAndUTF8(t *testing.T) {
	for _, mode := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m, domain.AnthropicCache1h} {
		t.Run(string(mode), func(t *testing.T) {
			policy, _ := domain.NewCachePolicy(mode, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
			testCompactionCacheStreams(t, policy)
		})
	}
}
func testCompactionCacheStreams(t *testing.T, policy domain.CachePolicy) {
	for _, trigger := range []domain.CompactionTrigger{domain.CompactionManual, domain.CompactionAuto} {
		for _, mode := range []string{"success", "refusal", "incomplete", "tool", "empty", "disconnect", "http"} {
			t.Run(string(trigger)+"/"+mode, func(t *testing.T) {
				var requests atomic.Int32
				events := []string{
					`{"type":"message_start","message":{"id":"summary","type":"message","role":"assistant","model":"model","content":[],"usage":{"input_tokens":100,"output_tokens":0}}}`,
					`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
					`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"修复 Error 后继续"}}`,
					`{"type":"content_block_stop","index":0}`,
					`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`,
					`{"type":"message_stop"}`,
				}
				switch mode {
				case "refusal":
					events[4] = `{"type":"message_delta","delta":{"stop_reason":"refusal"}}`
				case "incomplete":
					events[4] = `{"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`
				case "tool":
					events[1] = `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call","name":"exec_command","input":{}}}`
				case "empty":
					events[2] = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" "}}`
				case "disconnect":
					events = events[:4]
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					body, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(body), `"thinking":{"type":"adaptive","display":"omitted"}`) || !strings.Contains(string(body), `"output_config":{"effort":"high"}`) || !strings.Contains(string(body), `"max_tokens":4096`) {
						t.Error("summary lost frozen settings")
					}
					if mode == "http" {
						http.Error(w, "private-secret", http.StatusBadRequest)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					for _, event := range events {
						// 按单字节切分，覆盖 UTF-8 边界与半包。
						for _, b := range []byte("data: " + event + "\n\n") {
							_, _ = w.Write([]byte{b})
							w.(http.Flusher).Flush()
						}
					}
				}))
				defer server.Close()
				thinking, _ := domain.NewThinkingSettings(domain.ThinkingAdaptive, 0, domain.DisplayOmitted)
				settings, _ := domain.NewAnthropicInference(thinking, "high", 16384)
				instance, err := New(Config{Inference: settings, CachePolicy: policy, BaseURL: server.URL, APIKey: secret.New("private-secret"), Model: "model"})
				if err != nil {
					t.Fatal(err)
				}
				defer instance.Close()
				conversation := testConversation(t, instance)
				for _, entry := range testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage(strings.Repeat("old history ", 100)))}) {
					conversation.history.commit(entry)
				}
				before, _ := conversation.HistoryFootprint()
				input, _ := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
				compact, _ := provider.NewCompactionInput(input, "", 4096)
				if trigger == domain.CompactionAuto {
					scope, _ := domain.NewAutoCompactionScope("01900000-0000-7000-8000-000000000099", 1)
					compact, _ = compact.WithScope(scope)
				}
				preview, err := conversation.PreflightCompaction(compact)
				if err != nil {
					t.Fatal(err)
				}
				prepared, err := conversation.RunCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", preview)
				if requests.Load() != 1 {
					t.Fatalf("request retry: %d", requests.Load())
				}
				after, _ := conversation.HistoryFootprint()
				if before != after {
					t.Fatal("generation changed committed window")
				}
				if mode == "success" {
					if err != nil {
						t.Fatal(err)
					}
					view, _, _ := prepared.Preview()
					summary, _ := view.Summary()
					if summary.Text() != "修复 Error 后继续" {
						t.Fatal("UTF-8 summary lost")
					}
					if err := prepared.Discard(); err != nil {
						t.Fatal(err)
					}
				} else if err == nil || prepared != nil || strings.Contains(err.Error(), "private-secret") {
					t.Fatalf("invalid failure: %v", err)
				}
			})
		}
	}
}

// 取消必须等待 HTTP reader 退出后返回，不安装候选。
func TestCompactionStreamCancellation(t *testing.T) {
	policy, _ := domain.NewCachePolicy(domain.AnthropicCache1h, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
	for _, current := range []domain.CachePolicy{domain.DefaultCachePolicy(), policy} {
		fingerprint, _ := current.Fingerprint()
		t.Run(fingerprint, func(t *testing.T) { testCompactionStreamCancellation(t, current) })
	}
}
func testCompactionStreamCancellation(t *testing.T, policy domain.CachePolicy) {
	entered := make(chan struct{})
	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": ready\n\n")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(released)
	}))
	defer server.Close()
	thinking, _ := domain.NewThinkingSettings(domain.ThinkingAdaptive, 0, domain.DisplayOmitted)
	settings, _ := domain.NewAnthropicInference(thinking, "high", 16384)
	instance, err := New(Config{Inference: settings, CachePolicy: policy, BaseURL: server.URL, APIKey: secret.New("private-secret"), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := testConversation(t, instance)
	for _, entry := range testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, newUserMessage(strings.Repeat("old history ", 100)))}) {
		conversation.history.commit(entry)
	}
	input, _ := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
	compact, _ := provider.NewCompactionInput(input, "", 4096)
	preview, _ := conversation.PreflightCompaction(compact)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := conversation.RunCompaction(ctx, "01900000-0000-7000-8000-000000000009", preview)
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancellation accepted")
	}
	<-released
	if conversation.pending.Load() || conversation.active.Load() {
		t.Fatal("operation owner leaked")
	}
}

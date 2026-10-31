package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestCompactionStreamFailuresAndUTF8(t *testing.T) {
	for _, mode := range []string{"success", "refusal", "incomplete", "tool", "empty", "disconnect", "http"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			events := []string{
				`{"type":"response.created","response":{"id":"summary"}}`,
				`{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"修复 Error 后继续"}]}}`,
				`{"type":"response.completed","response":{"id":"summary","status":"completed"}}`,
			}
			switch mode {
			case "refusal":
				events[1] = `{"type":"response.refusal.delta","delta":"secret"}`
			case "incomplete":
				events[2] = `{"type":"response.completed","response":{"id":"summary","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`
			case "tool":
				events[1] = `{"type":"response.output_item.done","item":{"type":"function_call","id":"tool","call_id":"call","name":"exec_command","arguments":"{}"}}`
			case "empty":
				events[1] = `{"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":" "}]}}`
			case "disconnect":
				events = events[:2]
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
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
			instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("private-secret"), Model: "model"})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			conversation := instance.NewConversation().(*Conversation)
			for _, entry := range testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem(strings.Repeat("old history ", 100)))}) {
				conversation.history.commit(entry)
			}
			before, _ := conversation.HistoryFootprint()
			input, _ := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
			compact, _ := provider.NewCompactionInput(input, "", 4096)
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

// 取消必须等待 HTTP reader 退出后返回，不安装候选。
func TestCompactionStreamCancellation(t *testing.T) {
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
	instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("private-secret"), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	conversation := instance.NewConversation().(*Conversation)
	for _, entry := range testAssociatedHistory([]nativeHistoryEntry{testInputEntry(t, NewUserItem(strings.Repeat("old history ", 100)))}) {
		conversation.history.commit(entry)
	}
	input, _ := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
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

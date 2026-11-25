package testsupport

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
)

// CompactionConversation 保留实际 HTTP 流、原生摘要和操作 owner 的接线。
type CompactionConversation interface {
	provider.Conversation
	PreflightCompaction(provider.CompactionInput) (provider.CompactionPreview, provider.ContextWindowProjection, error)
	RunCompaction(context.Context, domain.CompactionID, provider.CompactionPreview) (*provider.PreparedCompaction, error)
	TestOperationBusy() bool
}

// CompactionFactory 每个场景建立独立 Provider，资源释放由测试 cleanup 持有。
type CompactionFactory func(*testing.T, string) (CompactionConversation, provider.CompactionInput)

// CheckCompactionStreams 按结果风险选样；缓存与触发方式的组合交给请求 golden。
func CheckCompactionStreams(t *testing.T, create CompactionFactory, frames func(string) []string, fragments []string) {
	t.Helper()
	for _, mode := range []string{"success", "auto-success", "refusal", "incomplete", "tool", "empty", "disconnect", "http"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				for _, fragment := range fragments {
					if !strings.Contains(string(body), fragment) {
						t.Error("summary lost frozen settings", fragment)
					}
				}
				if mode == "http" {
					http.Error(w, "private-secret", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range frames(mode) {
					// 单字节发送仍验证实际 HTTP/SSE 的 UTF-8 半包接线。
					for _, b := range []byte("data: " + event + "\n\n") {
						_, _ = w.Write([]byte{b})
						w.(http.Flusher).Flush()
					}
				}
			}))
			t.Cleanup(server.Close)
			conversation, compact := create(t, server.URL)
			_, before, _ := conversation.ProjectContextWindow()
			if mode == "auto-success" {
				scope, _ := domain.NewAutoCompactionScope("01900000-0000-7000-8000-000000000099", 1)
				compact, _ = compact.WithScope(scope)
			}
			preview, _, err := conversation.PreflightCompaction(compact)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := conversation.RunCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", preview)
			if requests.Load() != 1 {
				t.Fatalf("request retry: %d", requests.Load())
			}
			_, after, _ := conversation.ProjectContextWindow()
			if before != after {
				t.Fatal("generation changed committed window")
			}
			if mode == "success" || mode == "auto-success" {
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

// CheckCompactionCancellation 必须等到真实 HTTP reader 退出，且不遗留操作 owner。
func CheckCompactionCancellation(t *testing.T, create CompactionFactory) {
	t.Helper()
	entered, released := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": ready\n\n")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(released)
	}))
	t.Cleanup(server.Close)
	conversation, compact := create(t, server.URL)
	preview, _, err := conversation.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
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
	if conversation.TestOperationBusy() {
		t.Fatal("operation owner leaked")
	}
}

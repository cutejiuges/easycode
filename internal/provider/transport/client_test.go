package transport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewSSESourceUsesRestyAndStableJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method: got %s want POST", request.Method)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != `{"a":2,"z":1}` {
			t.Errorf("body: got %s", body)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("id: event-1\ndata: hello\n\n"))
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := NewClient(server.URL)
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
	}()

	received := make(chan SSEEvent, 1)
	var sourceCloser interface{ Close() }
	source, err := client.NewSSESource(ctx, SSERequest{
		Path: "/stream",
		Body: map[string]any{"z": 1, "a": 2},
	}, func(event SSEEvent) {
		received <- event
		if sourceCloser != nil {
			sourceCloser.Close()
		}
	})
	if err != nil {
		t.Fatalf("create SSE source: %v", err)
	}
	sourceCloser = source
	source.SetRetryCount(0)

	if err := source.Get(); err != nil {
		t.Fatalf("consume SSE source: %v", err)
	}

	select {
	case event := <-received:
		if event.ID != "event-1" || event.Data != "hello" {
			t.Fatalf("unexpected SSE event: %#v", event)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for SSE event")
	}
}

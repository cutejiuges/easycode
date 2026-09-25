package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveURLPreservesBasePrefix(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		path    string
		want    string
	}{
		{name: "host only", baseURL: "https://gateway.example", path: "responses", want: "https://gateway.example/responses"},
		{name: "path prefix", baseURL: "https://gateway.example/openai/v1", path: "responses", want: "https://gateway.example/openai/v1/responses"},
		{name: "trailing slash", baseURL: "https://gateway.example/openai/v1/", path: "/responses", want: "https://gateway.example/openai/v1/responses"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(test.baseURL)
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			defer closeClient(t, client)

			got, err := client.resolveURL(test.path)
			if err != nil {
				t.Fatalf("resolve URL: %v", err)
			}
			if got != test.want {
				t.Fatalf("resolved URL: got %q want %q", got, test.want)
			}
		})
	}
}

func TestNewClientRejectsAmbiguousBaseURL(t *testing.T) {
	tests := []string{
		"",
		"gateway.example/v1",
		"https://:443/v1",
		"ftp://gateway.example/v1",
		"https://user:secret@gateway.example/v1",
		"https://gateway.example/v1?token=secret",
		"https://gateway.example/v1#fragment",
	}
	for _, rawURL := range tests {
		t.Run(rawURL, func(t *testing.T) {
			_, err := NewClient(rawURL)
			if err == nil {
				t.Fatal("expected invalid base URL")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("error leaked URL secret: %v", err)
			}
		})
	}
}

func TestStreamSSEUsesStableJSONAndHeaders(t *testing.T) {
	receivedRequest := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method: got %s want POST", request.Method)
		}
		if request.URL.Path != "/api/v1/responses" {
			t.Errorf("path: got %s", request.URL.Path)
		}
		if request.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("accept: got %s", request.Header.Get("Accept"))
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("authorization header missing")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != `{"a":2,"z":1}` {
			t.Errorf("body: got %s", body)
		}
		receivedRequest <- struct{}{}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("id: event-1\nevent: delta\ndata: hello\n\n"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL + "/api/v1")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer closeClient(t, client)

	stream, err := client.StreamSSE(context.Background(), SSERequest{
		Path:    "responses",
		Headers: map[string]string{"Authorization": "Bearer test-key"},
		Body:    map[string]any{"z": 1, "a": 2},
	}, StreamOptions{IdleTimeout: time.Second})
	if err != nil {
		t.Fatalf("stream SSE: %v", err)
	}

	message := <-stream
	if message.Event == nil || message.Event.ID != "event-1" || message.Event.Name != "delta" || message.Event.Data != "hello" {
		t.Fatalf("unexpected SSE message: %#v", message)
	}
	terminal := <-stream
	if !errors.Is(terminal.Err, io.EOF) {
		t.Fatalf("terminal error: got %v want EOF", terminal.Err)
	}
	<-receivedRequest
}

func TestStreamSSEHTTPErrorDoesNotLeakSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":"top-secret"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer closeClient(t, client)

	_, err = client.StreamSSE(context.Background(), SSERequest{
		Path:    "responses",
		Headers: map[string]string{"Authorization": "Bearer top-secret"},
		Body:    map[string]string{"secret": "top-secret"},
	}, StreamOptions{})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("error leaked secret: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error omitted status: %v", err)
	}
}

func TestParseSSEFrameSemantics(t *testing.T) {
	input := strings.Join([]string{
		": heartbeat\r\n",
		"id: event-1\r\n",
		"event: delta\r\n",
		"data: 你\r\n",
		"data: 好\r\n",
		"\r\n",
		"data: final\n",
	}, "")

	events, err := collectParsedEvents(context.Background(), strings.NewReader(input), DefaultMaxEventBytes)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("parse error: got %v want EOF", err)
	}
	want := []SSEEvent{
		{ID: "event-1", Name: "delta", Data: "你\n好"},
		{Data: "final"},
	}
	if len(events) != len(want) {
		t.Fatalf("event count: got %d want %d", len(events), len(want))
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("event %d: got %#v want %#v", index, events[index], want[index])
		}
	}
}

func TestParseSSERandomChunkBoundaries(t *testing.T) {
	input := []byte("event: delta\ndata: 你好，world\n\ndata: done\n\n")
	want, err := collectParsedEvents(context.Background(), bytes.NewReader(input), DefaultMaxEventBytes)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("baseline parse: %v", err)
	}

	for chunkSize := 1; chunkSize <= len(input); chunkSize++ {
		reader := &fixedChunkReader{data: input, chunkSize: chunkSize}
		got, parseErr := collectParsedEvents(context.Background(), reader, DefaultMaxEventBytes)
		if !errors.Is(parseErr, io.EOF) {
			t.Fatalf("chunk %d parse: %v", chunkSize, parseErr)
		}
		if len(got) != len(want) {
			t.Fatalf("chunk %d event count: got %d want %d", chunkSize, len(got), len(want))
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("chunk %d event %d: got %#v want %#v", chunkSize, index, got[index], want[index])
			}
		}
	}
}

func TestParseSSERejectsOversizedEvent(t *testing.T) {
	_, err := collectParsedEvents(context.Background(), strings.NewReader("data: 123456789\n\n"), 8)
	if !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("error: got %v want event too large", err)
	}
}

func TestStreamSSECanBeCancelled(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer closeClient(t, client)

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.StreamSSE(ctx, SSERequest{Path: "responses", Body: struct{}{}}, StreamOptions{IdleTimeout: time.Second})
	if err != nil {
		t.Fatalf("stream SSE: %v", err)
	}
	<-requestStarted
	cancel()

	select {
	case message, open := <-stream:
		if open && !errors.Is(message.Err, context.Canceled) {
			t.Fatalf("cancel result: %#v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled stream did not exit")
	}
}

func TestStreamSSEIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer closeClient(t, client)

	stream, err := client.StreamSSE(context.Background(), SSERequest{Path: "responses", Body: struct{}{}}, StreamOptions{IdleTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("stream SSE: %v", err)
	}

	select {
	case message := <-stream:
		if message.Err == nil || !strings.Contains(message.Err.Error(), "idle timeout") {
			t.Fatalf("timeout result: %#v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("idle stream did not time out")
	}
}

func collectParsedEvents(ctx context.Context, reader io.Reader, maxEventBytes int) ([]SSEEvent, error) {
	activity := make(chan struct{}, 128)
	parsed := make(chan parsedSSEMessage, 128)
	err := parseSSE(ctx, reader, maxEventBytes, activity, parsed)
	close(parsed)
	events := make([]SSEEvent, 0)
	for message := range parsed {
		if message.event != nil {
			events = append(events, *message.event)
		}
	}
	return events, err
}

type fixedChunkReader struct {
	data      []byte
	offset    int
	chunkSize int
}

func (reader *fixedChunkReader) Read(target []byte) (int, error) {
	if reader.offset >= len(reader.data) {
		return 0, io.EOF
	}
	count := min(reader.chunkSize, len(target), len(reader.data)-reader.offset)
	copy(target, reader.data[reader.offset:reader.offset+count])
	reader.offset += count
	return count, nil
}

func closeClient(t *testing.T, client *Client) {
	t.Helper()
	if err := client.Close(); err != nil {
		t.Errorf("close client: %v", err)
	}
}

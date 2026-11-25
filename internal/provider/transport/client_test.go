package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/codec"
)

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

	body := mustCanonical(t, map[string]any{"z": 1, "a": 2}, DefaultMaxRequestBytes)
	stream, err := client.StreamSSE(context.Background(), SSERequest{
		Path:    "responses",
		Headers: map[string]string{"Authorization": "Bearer test-key"},
		Body:    body,
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

func TestStreamSSEDecodesGzipIncrementally(t *testing.T) {
	releaseTail := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.Header.Get("Accept-Encoding"), "gzip") {
			t.Error("request did not negotiate gzip")
		}
		compressed := flushGzipSSE(t, writer, "id: first\nevent: delta\ndata: 你好\n\n")
		select {
		case <-releaseTail:
		case <-request.Context().Done():
			return
		}
		if _, err := io.WriteString(compressed, "id: next\nevent: end\ndata: done\n\n"); err != nil {
			t.Errorf("write remaining gzip frame: %v", err)
		}
		if err := compressed.Close(); err != nil {
			t.Errorf("close gzip stream: %v", err)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := client.StreamSSE(ctx, SSERequest{Path: "responses", Body: mustCanonical(t, struct{}{}, DefaultMaxRequestBytes)}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first := awaitSSEMessage(t, ctx, stream); first.Err != nil || first.Event == nil ||
		*first.Event != (SSEEvent{ID: "first", Name: "delta", Data: "你好"}) {
		t.Fatalf("first compressed frame: %#v", first)
	}
	close(releaseTail)
	if next := awaitSSEMessage(t, ctx, stream); next.Err != nil || next.Event == nil ||
		*next.Event != (SSEEvent{ID: "next", Name: "end", Data: "done"}) {
		t.Fatalf("remaining compressed frame: %#v", next)
	}
	if terminal := awaitSSEMessage(t, ctx, stream); terminal.Event != nil || !errors.Is(terminal.Err, io.EOF) {
		t.Fatalf("gzip terminal: %#v", terminal)
	}
	assertSSEClosed(t, ctx, stream)
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

	body := mustCanonical(t, map[string]string{"secret": "top-secret"}, DefaultMaxRequestBytes)
	_, err = client.StreamSSE(context.Background(), SSERequest{
		Path:    "responses",
		Headers: map[string]string{"Authorization": "Bearer top-secret"},
		Body:    body,
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
	if !IsEventTooLarge(err) {
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
	body := mustCanonical(t, struct{}{}, DefaultMaxRequestBytes)
	stream, err := client.StreamSSE(ctx, SSERequest{Path: "responses", Body: body}, StreamOptions{IdleTimeout: time.Second})
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
	for _, name := range []string{"identity", "gzip"} {
		t.Run(name, func(t *testing.T) {
			compressed := name == "gzip"
			requestEnded := make(chan struct{})
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if requests.Add(1) == 1 {
					defer close(requestEnded)
				}
				if compressed {
					// 保持 gzip 成员未关闭，直到实际 HTTP 请求被取消。
					flushGzipSSE(t, writer, "data: first\n\n")
				} else {
					writer.Header().Set("Content-Type", "text/event-stream")
					writer.(http.Flusher).Flush()
				}
				<-request.Context().Done()
			}))
			defer server.Close()
			client, err := NewClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer closeClient(t, client)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			stream, err := client.StreamSSE(ctx, SSERequest{Path: "responses", Body: mustCanonical(t, struct{}{}, DefaultMaxRequestBytes)},
				StreamOptions{IdleTimeout: 30 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			if compressed {
				if first := awaitSSEMessage(t, ctx, stream); first.Err != nil || first.Event == nil || first.Event.Data != "first" {
					t.Fatalf("first compressed frame: %#v", first)
				}
			}
			if terminal := awaitSSEMessage(t, ctx, stream); terminal.Event != nil || !IsIdleTimeout(terminal.Err) {
				t.Fatalf("idle result: %#v", terminal)
			}
			assertSSEClosed(t, ctx, stream)
			select {
			case <-requestEnded:
				if count := requests.Load(); count != 1 {
					t.Fatalf("idle stream issued %d HTTP requests", count)
				}
			case <-ctx.Done():
				t.Fatal("idle stream did not cancel the HTTP request")
			}
		})
	}
}

func flushGzipSSE(t *testing.T, writer http.ResponseWriter, text string) *gzip.Writer {
	t.Helper()
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Content-Encoding", "gzip")
	compressed := gzip.NewWriter(writer)
	if _, err := io.WriteString(compressed, text); err != nil {
		t.Errorf("write gzip frame: %v", err)
	}
	if err := compressed.Flush(); err != nil {
		t.Errorf("flush gzip frame: %v", err)
	}
	writer.(http.Flusher).Flush()
	return compressed
}

func awaitSSEMessage(t *testing.T, ctx context.Context, stream <-chan SSEMessage) SSEMessage {
	t.Helper()
	select {
	case message, open := <-stream:
		if !open {
			t.Fatal("SSE stream closed before the expected message")
		}
		return message
	case <-ctx.Done():
		t.Fatal("SSE stream did not deliver the expected message")
		return SSEMessage{}
	}
}

func assertSSEClosed(t *testing.T, ctx context.Context, stream <-chan SSEMessage) {
	t.Helper()
	select {
	case message, open := <-stream:
		if open {
			t.Fatalf("unexpected message after SSE terminal: %#v", message)
		}
	case <-ctx.Done():
		t.Fatal("SSE stream did not close after its terminal")
	}
}

func TestStreamSSESendsCanonicalSnapshotAfterInputMutation(t *testing.T) {
	raw := []byte(`{"a":2,"z":1}`)
	body, err := codec.ParseCanonical(raw, DefaultMaxRequestBytes)
	if err != nil {
		t.Fatalf("parse canonical body: %v", err)
	}
	raw[2] = 'x'

	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestBody, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Errorf("read body: %v", readErr)
		}
		received <- requestBody
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: done\n\n"))
	}))
	defer server.Close()
	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer closeClient(t, client)

	stream, err := client.StreamSSE(context.Background(), SSERequest{Path: "responses", Body: body}, StreamOptions{})
	if err != nil {
		t.Fatalf("stream SSE: %v", err)
	}
	for range stream {
	}
	if got := string(<-received); got != `{"a":2,"z":1}` {
		t.Fatalf("sent body changed through caller input: %s", got)
	}
}

func collectParsedEvents(ctx context.Context, reader io.Reader, maxEventBytes int) ([]SSEEvent, error) {
	activity := make(chan struct{}, 128)
	parsed := make(chan parsedSSEMessage, 128)
	err := parseSSE(ctx, reader, StreamOptions{MaxEventBytes: maxEventBytes}, activity, parsed)
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

func mustCanonical(t *testing.T, value any, maxBytes int) codec.CanonicalJSON {
	t.Helper()
	canonical, err := codec.MarshalCanonical(value, maxBytes)
	if err != nil {
		t.Fatalf("marshal canonical JSON: %v", err)
	}
	return canonical
}

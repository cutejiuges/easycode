package transport

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/codec"
)

type blockedBody struct {
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
	reads   atomic.Int32
}

func (body *blockedBody) Read([]byte) (int, error) {
	if body.reads.Add(1) == 1 {
		close(body.entered)
	}
	<-body.closed
	return 0, io.EOF
}
func (body *blockedBody) Close() error { body.once.Do(func() { close(body.closed) }); return nil }

type fixedRoundTripper struct {
	body     io.ReadCloser
	encoding string
	calls    atomic.Int32
}

func (transport *fixedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	headers := make(http.Header)
	if transport.encoding != "" {
		headers.Set("Content-Encoding", transport.encoding)
	}
	return &http.Response{StatusCode: 200, Header: headers, Body: transport.body, Request: request}, nil
}

func TestCloseCancelsActualBlockedBody(t *testing.T) {
	client := newTestClient(t, "https://example.invalid")
	body := &blockedBody{entered: make(chan struct{}), closed: make(chan struct{})}
	transport := &fixedRoundTripper{body: body}
	client.HTTP().SetTransport(transport)
	canonical, err := codec.MarshalCanonical(struct{}{}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := client.StreamSSE(context.Background(), SSERequest{Path: "responses", Body: canonical}, StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-body.entered
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	for range messages {
	}
	<-body.closed
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StreamSSE(context.Background(), SSERequest{Path: "responses", Body: canonical}, StreamOptions{}); err == nil {
		t.Fatal("closed transport admitted request")
	}
	if transport.calls.Load() != 1 {
		t.Fatal("unexpected network call")
	}
}

func TestStreamSSEClosesBodyOnGzipDecoderError(t *testing.T) {
	client := newTestClient(t, "https://example.invalid")
	closer := &blockedBody{closed: make(chan struct{})}
	client.HTTP().SetTransport(&fixedRoundTripper{encoding: "gzip", body: struct {
		io.Reader
		io.Closer
	}{strings.NewReader("invalid gzip body"), closer}})
	request := SSERequest{Path: "responses", Body: mustCanonical(t, struct{}{}, 1024)}
	stream, err := client.StreamSSE(context.Background(), request, StreamOptions{})
	if stream != nil || !errors.Is(err, gzip.ErrHeader) {
		t.Fatalf("invalid gzip started a stream or returned the wrong error: %v", err)
	}
	select {
	case <-closer.closed:
	default:
		t.Fatal("transport returned without closing the body after decoder failure")
	}
}

func TestSSESealDeliversBufferedFramesAndClosesBody(t *testing.T) {
	client := newTestClient(t, "https://example.invalid")
	blocked := &blockedBody{entered: make(chan struct{}), closed: make(chan struct{})}
	reader := io.MultiReader(strings.NewReader("data: terminal\r\n\r\ndata: tail\r\n\r\ndata: incomplete\r\n"), blocked)
	client.HTTP().SetTransport(&fixedRoundTripper{body: struct {
		io.Reader
		io.Closer
	}{reader, blocked}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	messages, err := client.StreamSSE(ctx, SSERequest{Path: "responses", Body: mustCanonical(t, struct{}{}, 1024)}, StreamOptions{
		SealAfter: func(event SSEEvent) bool { return event.Data == "terminal" },
	})
	if err != nil {
		t.Fatal(err)
	}
	var delivered []string
	for message := range messages {
		if message.Event == nil || message.Err != nil {
			t.Fatalf("seal message = %#v", message)
		}
		delivered = append(delivered, message.Event.Data)
	}
	if strings.Join(delivered, ",") != "terminal,tail" || blocked.reads.Load() != 0 {
		t.Fatalf("seal delivered %q and made %d further reads", delivered, blocked.reads.Load())
	}
	select {
	case <-blocked.closed:
	default:
		t.Fatal("seal closed output before closing the body")
	}
}

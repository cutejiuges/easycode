package transport

import (
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

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
	body  io.ReadCloser
	calls atomic.Int32
}

func (transport *fixedRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: transport.body, Request: request}, nil
}

func TestCloseCancelsActualBlockedBody(t *testing.T) {
	client, err := NewClient("https://example.invalid")
	if err != nil {
		t.Fatal(err)
	}
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

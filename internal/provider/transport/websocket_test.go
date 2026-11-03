package transport

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"github.com/coder/websocket"
)

func wsTestClassifier(data []byte) (WSFrameInfo, error) {
	return WSFrameInfo{Name: "test", Terminal: true, Reusable: true}, nil
}
func wsTestBody(t *testing.T) codec.CanonicalJSON {
	t.Helper()
	body, err := codec.MarshalCanonical(struct {
		Type string `json:"type"`
	}{"response.create"}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
func TestWSClientFramesLimitsAndClose(t *testing.T) {
	for _, test := range []struct {
		name     string
		kind     websocket.MessageType
		data     []byte
		valid    bool
		fragment bool
	}{
		{"text", websocket.MessageText, []byte(`{"text":"中文"}`), true, false},
		{"fragmented", websocket.MessageText, []byte(`{"text":"中文"}`), true, true},
		{"binary", websocket.MessageBinary, []byte(`{}`), false, false},
		{"utf8", websocket.MessageText, []byte{'"', 0xff, '"'}, false, false},
		{"json", websocket.MessageText, []byte(`{"a":`), false, false},
		{"oversized", websocket.MessageText, []byte(`"` + strings.Repeat("a", DefaultMaxEventBytes) + `"`), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			sent := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				if _, _, err := conn.Read(r.Context()); err != nil {
					return
				}
				if test.fragment {
					writer, err := conn.Writer(r.Context(), test.kind)
					if err == nil {
						for _, b := range test.data {
							_, _ = writer.Write([]byte{b})
						}
						_ = writer.Close()
					}
				} else {
					_ = conn.Write(r.Context(), test.kind, test.data)
				}
				close(sent)
				_, _, _ = conn.Read(r.Context())
			}))
			defer server.Close()
			client, err := NewWSClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			conn, err := client.Open(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := conn.Stream(context.Background(), wsTestBody(t), time.Minute, wsTestClassifier)
			if err != nil {
				t.Fatal(err)
			}
			var event bool
			var failure error
			for message := range stream {
				event = event || message.Event != nil
				if message.Err != nil {
					failure = message.Err
				}
			}
			<-sent
			if test.valid && (!event || failure != nil || conn.Closed()) {
				t.Fatalf("valid frame failed: %v", failure)
			}
			if !test.valid && (failure == nil || !conn.Closed()) {
				t.Fatal("invalid frame was accepted")
			}
			_ = conn.Close()
			_ = conn.Close()
		})
	}
}
func TestWSClientActiveCloseAndIdleReuse(t *testing.T) {
	requests := make(chan struct{}, 3)
	release := make(chan struct{})
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for n := 0; n < 2; n++ {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
			requests <- struct{}{}
			if n == 0 {
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{}`))
			} else {
				<-release
			}
		}
	}))
	defer server.Close()
	client, _ := NewWSClient(server.URL)
	conn, err := client.Open(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := conn.Stream(context.Background(), wsTestBody(t), time.Minute, wsTestClassifier)
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	<-requests
	if conn.Closed() {
		t.Fatal("successful sample closed idle connection")
	}
	stream, err = conn.Stream(context.Background(), wsTestBody(t), time.Minute, wsTestClassifier)
	if err != nil {
		t.Fatal(err)
	}
	<-requests
	if _, err := conn.Stream(context.Background(), wsTestBody(t), time.Minute, wsTestClassifier); err == nil {
		t.Fatal("concurrent reader admitted")
	}
	_ = client.Close()
	for range stream {
	}
	select {
	case <-conn.done:
	default:
		t.Fatal("owner did not complete")
	}
	_ = client.Close()
	close(release)
	<-handlerDone
	if _, err := client.Open(context.Background(), nil); err == nil {
		t.Fatal("closed client reopened")
	}
}
func TestWSClientDeadlinesAreInjected(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, _, _ = conn.Read(r.Context())
		close(started)
		<-release
	}))
	defer server.Close()
	client, _ := NewWSClient(server.URL)
	defer client.Close()
	conn, err := client.Open(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	expired := make(chan struct{})
	client.deadline = func(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
		if d == 37*time.Second {
			<-started
			close(expired)
			ctx, cancel := context.WithDeadline(parent, time.Unix(0, 0))
			return ctx, cancel
		}
		if d != 30*time.Second {
			t.Errorf("wrong deadline: %s", d)
		}
		return context.WithCancel(parent)
	}
	stream, err := conn.Stream(context.Background(), wsTestBody(t), 37*time.Second, wsTestClassifier)
	if err != nil {
		t.Fatal(err)
	}
	var failure error
	for m := range stream {
		failure = m.Err
	}
	<-expired
	close(release)
	if !IsIdleTimeout(failure) {
		t.Fatalf("idle timeout not reported: %v", failure)
	}
}
func TestWSClientHandshakeSecurity(t *testing.T) {
	for _, status := range []int{404, 405, 426, 401, 403, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "http://127.0.0.1:1/private")
				w.WriteHeader(status)
			}))
			defer server.Close()
			client, _ := NewWSClient(server.URL)
			defer client.Close()
			_, err := client.Open(context.Background(), http.Header{"Authorization": []string{"Bearer private"}})
			want := domain.FallbackNone
			if status == 404 || status == 405 || status == 426 {
				want = domain.FallbackHandshake
			}
			if err == nil || WebSocketFallback(err) != want || strings.Contains(err.Error(), "private") {
				t.Fatalf("invalid safe error: %v", err)
			}
		})
	}
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tls.Close()
	client, _ := NewWSClient(tls.URL)
	defer client.Close()
	if _, err := client.Open(context.Background(), nil); err == nil || WebSocketFallback(err) != domain.FallbackNone {
		t.Fatal("TLS failure fell back")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Open(ctx, nil); !errors.Is(err, context.Canceled) || WebSocketFallback(err) != domain.FallbackNone {
		t.Fatal("cancel fell back")
	}
}

func TestWSClientTrustedTLSAndOpeningClose(t *testing.T) {
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if _, _, err = conn.Read(r.Context()); err == nil {
			_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{}`))
		}
		_, _, _ = conn.Read(r.Context())
	}))
	defer tls.Close()
	client, _ := NewWSClient(tls.URL)
	client.httpClient = tls.Client()
	defer client.Close()
	conn, err := client.Open(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := conn.Stream(context.Background(), wsTestBody(t), time.Minute, wsTestClassifier)
	if err != nil {
		t.Fatal(err)
	}
	for m := range stream {
		if m.Err != nil {
			t.Fatal(m.Err)
		}
	}
	_ = conn.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release }))
	defer server.Close()
	opening, _ := NewWSClient(server.URL)
	finished := make(chan error, 1)
	go func() { _, err := opening.Open(context.Background(), nil); finished <- err }()
	<-entered
	if err := opening.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err == nil {
		t.Fatal("close did not cancel opening")
	}
	close(release)
	select {
	case <-opening.done:
	default:
		t.Fatal("opening ownership abandoned")
	}
}

type blockedUpgrade struct {
	entered   chan struct{}
	closed    chan struct{}
	writeOnce sync.Once
	closeOnce sync.Once
}

func (b *blockedUpgrade) Read([]byte) (int, error) { <-b.closed; return 0, net.ErrClosed }
func (b *blockedUpgrade) Write([]byte) (int, error) {
	b.writeOnce.Do(func() { close(b.entered) })
	<-b.closed
	return 0, net.ErrClosed
}
func (b *blockedUpgrade) Close() error { b.closeOnce.Do(func() { close(b.closed) }); return nil }

type upgradeRoundTrip struct{ body *blockedUpgrade }

func (r upgradeRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	sum := sha1.Sum([]byte(req.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	headers := make(http.Header)
	headers.Set("Connection", "Upgrade")
	headers.Set("Upgrade", "websocket")
	headers.Set("Sec-WebSocket-Accept", base64.StdEncoding.EncodeToString(sum[:]))
	return &http.Response{StatusCode: 101, Header: headers, Body: r.body}, nil
}
func TestWSClientCloseUnblocksOriginalWriteOwner(t *testing.T) {
	body := &blockedUpgrade{entered: make(chan struct{}), closed: make(chan struct{})}
	client, _ := NewWSClient("https://example.invalid")
	client.httpClient = &http.Client{Transport: upgradeRoundTrip{body: body}}
	conn, err := client.Open(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := conn.Stream(context.Background(), wsTestBody(t), time.Minute, wsTestClassifier)
	if err != nil {
		t.Fatal(err)
	}
	<-body.entered
	_ = client.Close()
	for range stream {
	}
	select {
	case <-conn.done:
	default:
		t.Fatal("write owner was abandoned")
	}
	select {
	case <-body.closed:
	default:
		t.Fatal("underlying connection was not closed")
	}
	_ = client.Close()
}

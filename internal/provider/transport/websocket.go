package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"github.com/coder/websocket"
)

// WSFrameInfo 是 Provider 对事件边界的纯解析结果。
type WSFrameInfo struct {
	Name     string
	Terminal bool
	Reusable bool
}

// WSFrameClassifier 只解析有界事件，不执行外部操作。
type WSFrameClassifier func([]byte) (WSFrameInfo, error)

// WSClient 持有实例级连接登记和最终关闭信号。
type WSClient struct {
	mu         sync.Mutex
	endpoint   string
	entries    map[*WSConnection]struct{}
	closed     bool
	done       chan struct{}
	err        error
	deadline   func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	httpClient *http.Client
}

// WSConnection 是由单个 Conversation 拥有的连接和活动请求状态。
type WSConnection struct {
	mu         sync.Mutex
	client     *WSClient
	raw        *websocket.Conn
	opening    context.CancelFunc
	cancel     context.CancelFunc
	activeDone chan struct{}
	closed     bool
	done       chan struct{}
	finishOnce sync.Once
	socketOnce sync.Once
	closeErr   error
}
type wsOpenError struct {
	fallback  domain.CacheFallbackReason
	cancelled error
}

func (e *wsOpenError) Error() string { return "websocket connection failed" }
func (e *wsOpenError) Unwrap() error { return e.cancelled }

// WebSocketFallback 返回仅在生成请求写入前允许的回退原因。
func WebSocketFallback(err error) domain.CacheFallbackReason {
	var failure *wsOpenError
	if errors.As(err, &failure) {
		return failure.fallback
	}
	return domain.FallbackNone
}

// NewWSClient 只校验并构造连接登记，不打开网络。
func NewWSClient(baseURL string) (*WSClient, error) {
	parsed, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	parsed.Path += "/responses"
	parsed.RawPath = ""
	if parsed.Scheme == "https" {
		parsed.Scheme = "wss"
	} else {
		parsed.Scheme = "ws"
	}
	return &WSClient{endpoint: parsed.String(), entries: make(map[*WSConnection]struct{}), done: make(chan struct{}), deadline: context.WithTimeout, httpClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Open 显式建立并登记一条可取消的连接。
func (client *WSClient) Open(ctx context.Context, headers http.Header) (*WSConnection, error) {
	if ctx == nil {
		return nil, fmt.Errorf("websocket context is required")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	dialCtx, cancel := client.deadline(ctx, 30*time.Second)
	conn := &WSConnection{client: client, opening: cancel, done: make(chan struct{})}
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("websocket client is closed")
	}
	client.entries[conn] = struct{}{}
	client.mu.Unlock()
	raw, response, err := websocket.Dial(dialCtx, client.endpoint, &websocket.DialOptions{HTTPClient: client.httpClient, HTTPHeader: headers.Clone(), CompressionMode: websocket.CompressionDisabled})
	cancel()
	if err != nil {
		conn.mu.Lock()
		conn.opening = nil
		conn.closed = true
		conn.mu.Unlock()
		conn.finish()
		failure := &wsOpenError{fallback: domain.FallbackNone}
		if ctx.Err() != nil {
			failure.cancelled = ctx.Err()
			return nil, failure
		}
		if response != nil {
			switch response.StatusCode {
			case 404, 405, 426:
				failure.fallback = domain.FallbackHandshake
			}
		} else {
			var operation *net.OpError
			var dns *net.DNSError
			if errors.As(err, &dns) || (errors.As(err, &operation) && operation.Op == "dial") {
				failure.fallback = domain.FallbackConnect
			}
		}
		return nil, failure
	}
	raw.SetReadLimit(DefaultMaxEventBytes)
	conn.mu.Lock()
	conn.raw = raw
	conn.opening = nil
	closed := conn.closed
	conn.mu.Unlock()
	if closed || ctx.Err() != nil {
		conn.stop()
		conn.closeSocket()
		conn.finish()
		return nil, context.Canceled
	}
	return conn, nil
}
func (conn *WSConnection) finish() {
	conn.finishOnce.Do(func() {
		conn.client.mu.Lock()
		delete(conn.client.entries, conn)
		conn.client.mu.Unlock()
		close(conn.done)
	})
}
func (conn *WSConnection) closeSocket() {
	conn.mu.Lock()
	raw := conn.raw
	conn.mu.Unlock()
	if raw == nil {
		return
	}
	conn.socketOnce.Do(func() {
		if err := raw.CloseNow(); err != nil {
			conn.mu.Lock()
			conn.closeErr = fmt.Errorf("websocket close failed")
			conn.mu.Unlock()
		}
	})
}
func (conn *WSConnection) stop() {
	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		return
	}
	conn.closed = true
	opening, cancel, active := conn.opening, conn.cancel, conn.activeDone
	conn.mu.Unlock()
	if opening != nil {
		opening()
	}
	if cancel != nil {
		cancel()
	}
	conn.closeSocket()
	if opening == nil && active == nil {
		conn.finish()
	}
}

// Close 请求取消底层读写，等待原 owner 完成，并保留最终结果。
func (conn *WSConnection) Close() error {
	if conn == nil {
		return nil
	}
	conn.stop()
	<-conn.done
	conn.mu.Lock()
	err := conn.closeErr
	conn.mu.Unlock()
	return err
}

// Closed 返回有界内存状态，不访问网络。
func (conn *WSConnection) Closed() bool {
	if conn == nil {
		return true
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.closed
}

// Close 先取消所有登记资源，再等待既有最终完成信号。
func (client *WSClient) Close() error {
	client.mu.Lock()
	if client.closed {
		done := client.done
		client.mu.Unlock()
		<-done
		return client.err
	}
	client.closed = true
	entries := make([]*WSConnection, 0, len(client.entries))
	for conn := range client.entries {
		entries = append(entries, conn)
	}
	client.mu.Unlock()
	for _, conn := range entries {
		conn.stop()
	}
	var result error
	for _, conn := range entries {
		<-conn.done
		conn.mu.Lock()
		result = errors.Join(result, conn.closeErr)
		conn.mu.Unlock()
	}
	client.err = result
	close(client.done)
	return result
}

// Stream 由唯一请求 owner 发送一条生成请求并读取到原生终态。
func (conn *WSConnection) Stream(ctx context.Context, body codec.CanonicalJSON, idle time.Duration, classify WSFrameClassifier) (<-chan SSEMessage, error) {
	if ctx == nil || classify == nil {
		return nil, fmt.Errorf("websocket stream input is invalid")
	}
	if err := body.Validate(DefaultMaxRequestBytes); err != nil {
		return nil, fmt.Errorf("websocket request is invalid")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if idle <= 0 {
		idle = DefaultIdleTimeout
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	conn.mu.Lock()
	if conn.closed || conn.raw == nil || conn.activeDone != nil {
		conn.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("websocket connection is unavailable")
	}
	conn.cancel = cancel
	conn.activeDone = done
	raw := conn.raw
	conn.mu.Unlock()
	output := make(chan SSEMessage, 1)
	go conn.exchange(runCtx, cancel, raw, body, idle, classify, done, output)
	return output, nil
}
func (conn *WSConnection) exchange(ctx context.Context, cancel context.CancelFunc, raw *websocket.Conn, body codec.CanonicalJSON, idle time.Duration, classify WSFrameClassifier, done chan struct{}, output chan<- SSEMessage) {
	reusable := false
	defer func() {
		cancel()
		conn.mu.Lock()
		if !reusable {
			conn.closed = true
		}
		closed := conn.closed
		conn.mu.Unlock()
		if closed {
			conn.closeSocket()
		}
		conn.mu.Lock()
		conn.cancel = nil
		conn.activeDone = nil
		close(output)
		close(done)
		closed = conn.closed
		conn.mu.Unlock()
		if closed {
			conn.closeSocket()
			conn.finish()
		}
	}()

	emitError := func(err error) {
		select {
		case output <- SSEMessage{Err: err}:
		case <-ctx.Done():
		}
	}
	writeCtx, stopWrite := conn.client.deadline(ctx, 30*time.Second)
	err := raw.Write(writeCtx, websocket.MessageText, body.Bytes())
	stopWrite()
	if err != nil {
		emitError(fmt.Errorf("websocket request write failed"))
		return
	}
	for {
		readCtx, stopRead := conn.client.deadline(ctx, idle)
		kind, data, readErr := raw.Read(readCtx)
		timedOut := readCtx.Err() == context.DeadlineExceeded
		stopRead()
		if readErr != nil {
			if timedOut && ctx.Err() == nil {
				emitError(errIdleTimeout)
			} else {
				emitError(fmt.Errorf("websocket stream read failed"))
			}
			return
		}
		if kind != websocket.MessageText || len(data) > DefaultMaxEventBytes || !utf8.Valid(data) || codec.ValidateJSON(data) != nil {
			emitError(fmt.Errorf("websocket event is invalid"))
			return
		}
		info, err := classify(data)
		if err != nil {
			emitError(err)
			return
		}
		if info.Terminal {
			reusable = info.Reusable
		}
		select {
		case output <- SSEMessage{Event: &SSEEvent{Name: info.Name, Data: string(data)}}:
		case <-ctx.Done():
			reusable = false
			return
		}
		if info.Terminal {
			return
		}
	}
}

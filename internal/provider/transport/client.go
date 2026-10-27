// Package transport 封装 Resty v3 的 HTTP 和 SSE 基础设施。
package transport

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"resty.dev/v3"

	"easycode/internal/codec"
)

const (
	// DefaultMaxRequestBytes 限制单个已编译 JSON 请求正文的最大字节数。
	DefaultMaxRequestBytes = 16 << 20
	// DefaultMaxEventBytes 限制单个 SSE event 占用的最大字节数。
	DefaultMaxEventBytes = 4 << 20
	// DefaultIdleTimeout 限制已经建立的流长时间没有任何数据或心跳。
	DefaultIdleTimeout = 5 * time.Minute
)

// Client 统一持有 Resty HTTP 客户端和经过校验的 base URL。
type Client struct {
	baseURL   *url.URL
	http      *resty.Client
	mu        sync.Mutex
	active    map[*streamLifetime]context.CancelFunc
	closed    bool
	closeDone chan struct{}
	closeErr  error
}

// SSERequest 描述一条返回 SSE raw body 的 HTTP 请求。
type SSERequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    codec.CanonicalJSON
}

// StreamOptions 控制 SSE frame 大小和空闲超时。
type StreamOptions struct {
	MaxEventBytes int
	IdleTimeout   time.Duration
}

// NewClient 创建 Resty v3 客户端并校验 base URL。
func NewClient(baseURL string) (*Client, error) {
	parsedBaseURL, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	httpClient := resty.New().SetRetryCount(0)
	return &Client{
		baseURL: parsedBaseURL,
		http:    httpClient,
		active:  make(map[*streamLifetime]context.CancelFunc), closeDone: make(chan struct{}),
	}, nil
}

// HTTP 返回只供 provider transport 边界使用的 Resty 客户端。
func (client *Client) HTTP() *resty.Client {
	return client.http
}

// streamLifetime 用实例身份登记活动请求，不保存长期 Context。
type streamLifetime struct{ active bool }

// Close 取消全部活动请求，使 SSE owner 关闭实际 body，再关闭空闲连接。
func (client *Client) Close() error {
	client.mu.Lock()
	if client.closed {
		done := client.closeDone
		client.mu.Unlock()
		<-done
		return client.closeErr
	}
	client.closed = true
	cancels := make([]context.CancelFunc, 0, len(client.active))
	for _, cancel := range client.active {
		cancels = append(cancels, cancel)
	}
	client.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	client.closeErr = client.http.Close()
	close(client.closeDone)
	return client.closeErr
}

func (client *Client) track(ctx context.Context) (context.Context, func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	lifetime := &streamLifetime{active: true}
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		cancel()
		return nil, nil, fmt.Errorf("transport is closed")
	}
	client.active[lifetime] = cancel
	client.mu.Unlock()
	return ctx, func() {
		cancel()
		client.mu.Lock()
		delete(client.active, lifetime)
		client.mu.Unlock()
	}, nil
}

// StreamSSE 发起流式请求并返回单一 owner 管理的有序消息 channel。
func (client *Client) StreamSSE(
	ctx context.Context,
	request SSERequest,
	options StreamOptions,
) (<-chan SSEMessage, error) {
	resolvedURL, err := client.resolveURL(request.Path)
	if err != nil {
		return nil, err
	}

	method := request.Method
	if method == "" {
		method = http.MethodPost
	}
	if err := request.Body.Validate(DefaultMaxRequestBytes); err != nil {
		return nil, fmt.Errorf("streaming request body is invalid: %w", err)
	}
	body := request.Body.Bytes()

	ctx, finished, err := client.track(ctx)
	if err != nil {
		return nil, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			finished()
		}
	}()
	restyRequest := client.http.R().
		SetContext(ctx).
		SetResponseDoNotParse(true).
		SetRetryCount(0).
		SetBody(bytes.NewReader(body))

	headerNames := make([]string, 0, len(request.Headers))
	for name := range request.Headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	for _, name := range headerNames {
		restyRequest.SetHeader(name, request.Headers[name])
	}
	if restyRequest.Header.Get("Content-Type") == "" {
		restyRequest.SetHeader("Content-Type", "application/json")
	}
	if restyRequest.Header.Get("Accept") == "" {
		restyRequest.SetHeader("Accept", "text/event-stream")
	}

	response, err := restyRequest.Execute(method, resolvedURL)
	if err != nil {
		return nil, fmt.Errorf("open streaming request: %w", err)
	}
	if response.RawResponse == nil || response.RawResponse.Body == nil {
		return nil, fmt.Errorf("open streaming request: response body is unavailable")
	}
	if response.StatusCode() < http.StatusOK || response.StatusCode() >= http.StatusMultipleChoices {
		_ = response.RawResponse.Body.Close()
		return nil, fmt.Errorf("streaming request failed with HTTP status %d", response.StatusCode())
	}

	handedOff = true
	return superviseSSE(ctx, response.RawResponse.Body, normalizeOptions(options), finished), nil
}

func parseBaseURL(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	parsedURL, err := url.Parse(trimmed)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" || parsedURL.Hostname() == "" {
		return nil, fmt.Errorf("base URL is invalid")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("base URL scheme must be http or https")
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return nil, fmt.Errorf("base URL must not include user info, query, or fragment")
	}
	parsedURL.Path = strings.TrimRight(parsedURL.Path, "/")
	parsedURL.RawPath = strings.TrimRight(parsedURL.RawPath, "/")
	return parsedURL, nil
}

func (client *Client) resolveURL(endpoint string) (string, error) {
	trimmedEndpoint := strings.TrimSpace(endpoint)
	parsedEndpoint, err := url.Parse(trimmedEndpoint)
	if err != nil || parsedEndpoint.IsAbs() || parsedEndpoint.Host != "" {
		return "", fmt.Errorf("request endpoint must be a relative path")
	}
	if parsedEndpoint.RawQuery != "" || parsedEndpoint.Fragment != "" {
		return "", fmt.Errorf("request endpoint must not include query or fragment")
	}
	endpointPath := strings.Trim(parsedEndpoint.Path, "/")
	if endpointPath == "" {
		return "", fmt.Errorf("request endpoint is required")
	}

	resolved := *client.baseURL
	basePath := strings.TrimRight(resolved.Path, "/")
	resolved.Path = basePath + "/" + endpointPath
	resolved.RawPath = ""
	return resolved.String(), nil
}

func normalizeOptions(options StreamOptions) StreamOptions {
	if options.MaxEventBytes <= 0 {
		options.MaxEventBytes = DefaultMaxEventBytes
	}
	if options.IdleTimeout <= 0 {
		options.IdleTimeout = DefaultIdleTimeout
	}
	return options
}

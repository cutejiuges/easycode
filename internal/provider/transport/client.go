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
	"time"

	"resty.dev/v3"

	"easycode/internal/codec"
)

const (
	// DefaultMaxEventBytes 限制单个 SSE event 占用的最大字节数。
	DefaultMaxEventBytes = 4 << 20
	// DefaultIdleTimeout 限制已经建立的流长时间没有任何数据或心跳。
	DefaultIdleTimeout = 5 * time.Minute
)

// Client 统一持有 Resty HTTP 客户端和经过校验的 base URL。
type Client struct {
	baseURL *url.URL
	http    *resty.Client
}

// SSERequest 描述一条返回 SSE raw body 的 HTTP 请求。
type SSERequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    any
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
	}, nil
}

// HTTP 返回只供 provider transport 边界使用的 Resty 客户端。
func (client *Client) HTTP() *resty.Client {
	return client.http
}

// Close 关闭 Resty 持有的空闲连接和资源。
func (client *Client) Close() error {
	return client.http.Close()
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
	body, err := codec.MarshalStable(request.Body)
	if err != nil {
		return nil, fmt.Errorf("marshal streaming request body: %w", err)
	}

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

	return superviseSSE(ctx, response.RawResponse.Body, normalizeOptions(options)), nil
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

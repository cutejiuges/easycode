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

	"resty.dev/v3"

	"easycode/internal/codec"
)

// Client 统一持有 Resty HTTP 客户端和规范化 base URL。
type Client struct {
	baseURL string
	http    *resty.Client
}

// NewClient 创建 Resty v3 客户端。
func NewClient(baseURL string) *Client {
	normalizedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	httpClient := resty.New()
	if normalizedBaseURL != "" {
		httpClient.SetBaseURL(normalizedBaseURL)
	}
	return &Client{
		baseURL: normalizedBaseURL,
		http:    httpClient,
	}
}

// HTTP 返回只供 provider transport 边界使用的 Resty 客户端。
func (client *Client) HTTP() *resty.Client {
	return client.http
}

// Close 关闭 Resty 持有的空闲连接和资源。
func (client *Client) Close() error {
	return client.http.Close()
}

// SSERequest 描述一条由 Resty SSESource 发起的流式请求。
type SSERequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    any
}

// SSEEvent 是与 Resty 具体回调类型解耦后的原始事件。
type SSEEvent struct {
	ID   string
	Name string
	Data string
}

// NewSSESource 创建使用同一 Resty transport 的 SSE source。
func (client *Client) NewSSESource(
	ctx context.Context,
	request SSERequest,
	onEvent func(SSEEvent),
) (*resty.SSESource, error) {
	if onEvent == nil {
		return nil, fmt.Errorf("SSE event handler is required")
	}
	resolvedURL, err := client.resolveURL(request.Path)
	if err != nil {
		return nil, err
	}

	method := request.Method
	if method == "" {
		method = http.MethodPost
	}
	source := resty.NewSSESource().
		SetURL(resolvedURL).
		SetMethod(method).
		SetContext(ctx).
		SetTransport(client.http.Transport())

	headerNames := make([]string, 0, len(request.Headers))
	for name := range request.Headers {
		headerNames = append(headerNames, name)
	}
	sort.Strings(headerNames)
	for _, name := range headerNames {
		source.SetHeader(name, request.Headers[name])
	}

	if request.Body != nil {
		body, marshalErr := codec.MarshalStable(request.Body)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal SSE request body: %w", marshalErr)
		}
		source.SetHeader("Content-Type", "application/json")
		source.SetBody(bytes.NewReader(body))
	}

	source.OnMessage(func(value any) {
		event, ok := value.(*resty.SSE)
		if !ok || event == nil {
			return
		}
		onEvent(SSEEvent{ID: event.ID, Name: event.Name, Data: event.Data})
	}, nil)
	return source, nil
}

func (client *Client) resolveURL(path string) (string, error) {
	parsedPath, err := url.Parse(path)
	if err != nil {
		return "", fmt.Errorf("invalid request URL: %w", err)
	}
	if parsedPath.IsAbs() {
		return parsedPath.String(), nil
	}
	if client.baseURL == "" {
		return "", fmt.Errorf("base URL is required for relative request path")
	}
	parsedBaseURL, err := url.Parse(client.baseURL + "/")
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	return parsedBaseURL.ResolveReference(parsedPath).String(), nil
}

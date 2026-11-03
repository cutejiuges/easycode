package openai

import (
	"context"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider/transport"
	"fmt"
	"net/http"
)

type requestSelection struct {
	transport domain.RequestTransport
	mode      domain.RequestMode
	reason    domain.RequestResetReason
	fallback  domain.CacheFallbackReason
}

func (s requestSelection) apply(r domain.RequestReceipt) (domain.RequestReceipt, error) {
	r.Transport = s.transport
	r.Mode = s.mode
	r.Fallback = s.fallback
	if s.reason != domain.ResetNone {
		r.Reason = s.reason
	}
	return domain.NewRequestReceipt(r)
}

type websocketCreate struct {
	Type               string         `json:"type"`
	PreviousResponseID string         `json:"previous_response_id,omitempty"`
	PromptCacheKey     string         `json:"prompt_cache_key,omitempty"`
	ToolChoice         string         `json:"tool_choice,omitempty"`
	MaxOutputTokens    uint64         `json:"max_output_tokens,omitempty"`
	Model              string         `json:"model"`
	Input              []NativeItem   `json:"input"`
	Tools              []responseTool `json:"tools"`
	Store              bool           `json:"store"`
	Include            []string       `json:"include"`
}

func websocketBody(request codec.CanonicalJSON, responseID string, offset int) (codec.CanonicalJSON, error) {
	var r responsesRequest
	if err := codec.Unmarshal(request.Bytes(), &r); err != nil {
		return codec.CanonicalJSON{}, err
	}
	if offset < 0 || offset > len(r.Input) {
		return codec.CanonicalJSON{}, fmt.Errorf("incremental request offset is invalid")
	}
	return codec.MarshalCanonical(websocketCreate{Type: "response.create", PreviousResponseID: responseID, PromptCacheKey: r.PromptCacheKey, ToolChoice: r.ToolChoice, MaxOutputTokens: r.MaxOutputTokens, Model: r.Model, Input: r.Input[offset:], Tools: r.Tools, Store: false, Include: r.Include}, maxResponsesRequestBytes)
}
func classifyWebsocketFrame(data []byte) (transport.WSFrameInfo, error) {
	var event struct {
		Type string `json:"type"`
	}
	if err := codec.Unmarshal(data, &event); err != nil || event.Type == "" {
		return transport.WSFrameInfo{}, fmt.Errorf("websocket event type is invalid")
	}
	info := transport.WSFrameInfo{Name: event.Type}
	switch event.Type {
	case "response.completed":
		info.Terminal = true
		info.Reusable = true
	case "response.failed", "response.incomplete", "error":
		info.Terminal = true
	}
	return info, nil
}
func (conversation *Conversation) closeWebsocket() {
	if conversation.socket != nil {
		_ = conversation.socket.Close()
		conversation.socket = nil
	}
	conversation.wsResponseID = ""
}
func (conversation *Conversation) startRequest(ctx context.Context, request codec.CanonicalJSON, evidence requestEvidence, purpose domain.RequestPurpose) (<-chan transport.SSEMessage, requestSelection, func(), error) {
	selection := requestSelection{transport: domain.TransportHTTP, mode: domain.RequestFull, reason: domain.ResetNone, fallback: domain.FallbackNone}
	cleanup := func() {}
	if purpose == domain.RequestSummary {
		conversation.closeWebsocket()
	}
	if conversation.provider.config.CachePolicy.Transport() == domain.OpenAIWebSocketAuto && !conversation.httpFallback.Load() {
		conn := conversation.socket
		reset := false
		if conn != nil && conn.Closed() {
			conversation.closeWebsocket()
			conn = nil
			reset = true
		}
		if conn == nil {
			var err error
			conn, err = conversation.provider.websocket.Open(ctx, http.Header{"Authorization": []string{"Bearer " + conversation.provider.config.APIKey.Reveal()}})
			if err != nil {
				fallback := transport.WebSocketFallback(err)
				if fallback == domain.FallbackNone {
					return nil, selection, cleanup, err
				}
				conversation.httpFallback.Store(true)
				conversation.fallback = fallback
			} else if purpose == domain.RequestSample {
				conversation.socket = conn
			}
		}
		if conn != nil {
			selection.transport = domain.TransportWebSocket
			offset, _, reason := conversation.baseline.Compare(evidence.items, evidence.properties)
			selection.reason = reason
			responseID := ""
			if purpose == domain.RequestSample && conversation.wsResponseID != "" && reason == domain.ResetNone && conversation.nextReason == domain.ResetNone {
				responseID = conversation.wsResponseID
				selection.mode = domain.RequestIncremental
			} else {
				offset = 0
				if reset || reason == domain.ResetNone {
					selection.reason = domain.ResetConnection
				}
				if conversation.nextReason != "" && conversation.nextReason != domain.ResetNone {
					selection.reason = conversation.nextReason
				}
			}
			if purpose == domain.RequestSummary {
				offset = 0
				selection.reason = domain.ResetSummary
				cleanup = func() { _ = conn.Close() }
			}
			body, err := websocketBody(request, responseID, offset)
			if err != nil {
				_ = conn.Close()
				return nil, selection, cleanup, err
			}
			stream, err := conn.Stream(ctx, body, conversation.provider.config.StreamIdleTimeout, classifyWebsocketFrame)
			if err != nil {
				_ = conn.Close()
				return nil, selection, cleanup, err
			}
			return stream, selection, cleanup, nil
		}
	}
	if conversation.httpFallback.Load() {
		selection.fallback = conversation.fallback
	}
	stream, err := conversation.provider.transport.StreamSSE(ctx, transport.SSERequest{Method: http.MethodPost, Path: "responses", Headers: map[string]string{"Authorization": "Bearer " + conversation.provider.config.APIKey.Reveal(), "Accept": "text/event-stream", "Content-Type": "application/json"}, Body: request}, transport.StreamOptions{IdleTimeout: conversation.provider.config.StreamIdleTimeout})
	return stream, selection, cleanup, err
}

// ResetContinuation 由 turn owner 在失败或外层取消后清除瞬态连接资格。
func (conversation *Conversation) ResetContinuation() { conversation.closeWebsocket() }

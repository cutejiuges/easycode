package openai

import (
	"context"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
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
	responsesReasoningFields
	Type               string `json:"type"`
	PreviousResponseID string `json:"previous_response_id,omitempty"`
	responsesInputFields
	responsesStorageFields
}

func (plan samplingRequestPlan) websocketBody(responseID string, offset int) (codec.CanonicalJSON, error) {
	fields := plan.request.responsesInputFields
	if offset < 0 || offset > len(fields.Input) {
		return codec.CanonicalJSON{}, fmt.Errorf("incremental request offset is invalid")
	}
	fields.Input = fields.Input[offset:]
	return codec.MarshalCanonical(websocketCreate{
		responsesReasoningFields: plan.request.responsesReasoningFields,
		Type:                     "response.create",
		PreviousResponseID:       responseID,
		responsesInputFields:     fields,
		responsesStorageFields:   plan.request.responsesStorageFields,
	}, maxResponsesRequestBytes)
}

func responseFrameType(data []byte) (string, error) {
	var event struct {
		Type string `json:"type"`
	}
	err := codec.Unmarshal(data, &event)
	return event.Type, err
}

func classifyWebsocketFrame(data []byte) (transport.WSFrameInfo, error) {
	kind, err := responseFrameType(data)
	if err != nil || kind == "" {
		return transport.WSFrameInfo{}, fmt.Errorf("websocket event type is invalid")
	}
	info := transport.WSFrameInfo{Name: kind}
	switch kind {
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
func (conversation *Conversation) startRequest(ctx context.Context, plan samplingRequestPlan, request codec.CanonicalJSON, comparison provider.RequestComparison) (<-chan transport.SSEMessage, requestSelection, func(), error) {
	selection := requestSelection{transport: domain.TransportHTTP, mode: domain.RequestFull, reason: domain.ResetNone, fallback: domain.FallbackNone}
	cleanup := func() {}
	purpose := plan.purpose
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
			offset, reason := comparison.Suffix()
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
			body, err := plan.websocketBody(responseID, offset)
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
	options := transport.StreamOptions{IdleTimeout: conversation.provider.config.StreamIdleTimeout}
	if purpose == domain.RequestSample {
		options.SealAfter = func(event transport.SSEEvent) bool {
			// 只标记读取截止位置。终态合法性仍由有序 reducer 核验。
			kind, err := responseFrameType([]byte(event.Data))
			return err == nil && kind == "response.completed"
		}
	}
	stream, err := conversation.provider.transport.StreamSSE(ctx, transport.SSERequest{Method: http.MethodPost, Path: "responses", Headers: map[string]string{"Authorization": "Bearer " + conversation.provider.config.APIKey.Reveal(), "Accept": "text/event-stream", "Content-Type": "application/json"}, Body: request}, options)
	return stream, selection, cleanup, err
}

// ResetContinuation 由 turn owner 在失败或外层取消后清除瞬态连接资格。
func (conversation *Conversation) ResetContinuation() error {
	if conversation.operations.Begin() != provider.ConversationIdle {
		return fmt.Errorf("OpenAI conversation cannot reset continuation while busy")
	}
	defer conversation.operations.End()
	conversation.closeWebsocket()
	return nil
}

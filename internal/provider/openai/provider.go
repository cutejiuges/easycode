// Package openai 实现 OpenAI Responses Provider Kernel。
package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/secret"
)

// Config 描述 OpenAI Responses 连接信息。
type Config struct {
	BaseURL           string
	APIKey            secret.Value
	Model             string
	StreamIdleTimeout time.Duration
}

// Provider 持有不可变 OpenAI 配置和共享 Resty transport。
type Provider struct {
	config    Config
	transport *transport.Client
}

// Conversation 持有一条会话独占的 OpenAI 原生历史。
type Conversation struct {
	provider *Provider
	history  nativeHistory
	active   atomic.Bool
}

type nativeHistory struct {
	mu    sync.RWMutex
	items []NativeItem
}

// New 创建 OpenAI Provider。
func New(config Config) (*Provider, error) {
	if strings.TrimSpace(config.Model) == "" {
		return nil, fault.New(fault.CodeInvalidConfiguration, "OpenAI model is required")
	}
	if config.APIKey.Empty() {
		return nil, fault.New(fault.CodeInvalidConfiguration, "OpenAI API key is required")
	}
	client, err := transport.NewClient(config.BaseURL)
	if err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "OpenAI base URL is invalid", err)
	}
	return &Provider{config: config, transport: client}, nil
}

// Family 返回 OpenAI 协议家族。
func (*Provider) Family() domain.ProviderFamily {
	return domain.ProviderOpenAI
}

// Capabilities 返回当前文本切片已实现的能力集。
func (*Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Streaming:          true,
		EncryptedReasoning: true,
	}
}

// NewConversation 创建历史相互隔离的会话。
func (instance *Provider) NewConversation() provider.Conversation {
	return &Conversation{provider: instance}
}

// Close 释放 HTTP transport 资源。
func (instance *Provider) Close() error {
	return instance.transport.Close()
}

// Family 返回 OpenAI 协议家族。
func (*Conversation) Family() domain.ProviderFamily {
	return domain.ProviderOpenAI
}

// Capabilities 返回当前会话所属 Provider 的能力集。
func (conversation *Conversation) Capabilities() provider.Capabilities {
	return conversation.provider.Capabilities()
}

// Stream 发起一次 Responses 文本 turn。
func (conversation *Conversation) Stream(
	ctx context.Context,
	input provider.TurnInput,
) (<-chan provider.StreamEvent, error) {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil, fault.New(fault.CodeTurnFailed, "turn input is required")
	}
	if !conversation.active.CompareAndSwap(false, true) {
		return nil, fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}

	userItem := NewUserItem(text)
	request := compileResponsesRequest(
		conversation.provider.config.Model,
		conversation.history.snapshot(),
		userItem,
	)
	streamContext, cancelStream := context.WithCancel(ctx)
	stream, err := conversation.provider.transport.StreamSSE(streamContext, transport.SSERequest{
		Method: http.MethodPost,
		Path:   "responses",
		Headers: map[string]string{
			"Authorization": "Bearer " + conversation.provider.config.APIKey.Reveal(),
			"Accept":        "text/event-stream",
			"Content-Type":  "application/json",
		},
		Body: request,
	}, transport.StreamOptions{IdleTimeout: conversation.provider.config.StreamIdleTimeout})
	if err != nil {
		cancelStream()
		conversation.active.Store(false)
		if errors.Is(err, context.Canceled) {
			return nil, fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
		}
		return nil, fault.Wrap(fault.CodeProviderRequest, "provider request failed", err)
	}

	output := make(chan provider.StreamEvent, 16)
	go conversation.consumeStream(ctx, cancelStream, userItem, stream, output)
	return output, nil
}

func (conversation *Conversation) consumeStream(
	ctx context.Context,
	cancelStream context.CancelFunc,
	userItem NativeItem,
	stream <-chan transport.SSEMessage,
	output chan<- provider.StreamEvent,
) {
	defer close(output)
	defer conversation.active.Store(false)
	defer cancelStream()

	stagedItems := make([]NativeItem, 0)
	stopTransport := func() {
		cancelStream()
		for range stream {
		}
	}
	sendTerminal := func(kind provider.StreamEventKind, err error) {
		output <- provider.StreamEvent{Kind: kind, Err: err}
	}

	for message := range stream {
		if message.Event != nil {
			result, err := reduceResponsesEvent(*message.Event)
			if err != nil {
				stopTransport()
				sendTerminal(provider.StreamEventFailed, err)
				return
			}
			if result.semantic != nil {
				output <- provider.StreamEvent{Kind: provider.StreamEventSemantic, Event: *result.semantic}
			}
			if result.native != nil {
				item := result.native.clone()
				stagedItems = append(stagedItems, item)
				output <- provider.StreamEvent{Kind: provider.StreamEventNative, Native: item}
			}
			if result.completed {
				stopTransport()
				conversation.history.commit(userItem, stagedItems)
				sendTerminal(provider.StreamEventCompleted, nil)
				return
			}
			continue
		}

		if message.Err != nil {
			sendTerminal(mapTransportTerminal(ctx, message.Err))
			return
		}
	}

	if ctx.Err() != nil {
		sendTerminal(provider.StreamEventCancelled, fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled))
		return
	}
	sendTerminal(provider.StreamEventFailed, fault.New(fault.CodeStreamProtocol, "provider stream closed before response.completed"))
}

func mapTransportTerminal(ctx context.Context, err error) (provider.StreamEventKind, error) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return provider.StreamEventCancelled, fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
	}
	if errors.Is(err, transport.ErrIdleTimeout) {
		return provider.StreamEventFailed, fault.Wrap(fault.CodeStreamIdleTimeout, "provider stream idle timeout", err)
	}
	if errors.Is(err, io.EOF) {
		return provider.StreamEventFailed, fault.New(fault.CodeStreamProtocol, "provider stream closed before response.completed")
	}
	if errors.Is(err, transport.ErrEventTooLarge) {
		return provider.StreamEventFailed, fault.Wrap(fault.CodeStreamProtocol, "provider stream event is too large", err)
	}
	return provider.StreamEventFailed, fault.Wrap(fault.CodeStreamProtocol, "provider stream failed", err)
}

func (history *nativeHistory) snapshot() []NativeItem {
	history.mu.RLock()
	defer history.mu.RUnlock()
	items := make([]NativeItem, 0, len(history.items))
	for _, item := range history.items {
		items = append(items, item.clone())
	}
	return items
}

func (history *nativeHistory) commit(userItem NativeItem, outputItems []NativeItem) {
	history.mu.Lock()
	defer history.mu.Unlock()
	history.items = append(history.items, userItem.clone())
	for _, item := range outputItems {
		history.items = append(history.items, item.clone())
	}
}

func (conversation *Conversation) historySnapshot() []NativeItem {
	return conversation.history.snapshot()
}

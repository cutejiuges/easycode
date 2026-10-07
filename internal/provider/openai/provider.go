// Package openai 实现 OpenAI Responses Provider Kernel。
package openai

import (
	"context"
	"errors"
	"fmt"
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
	pending  atomic.Bool
}

type nativeHistory struct {
	mu       sync.RWMutex
	turns    []nativeTurn
	revision uint64
}

var _ provider.Conversation = (*Conversation)(nil)
var _ provider.Factory = (*Provider)(nil)

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

// RestoreConversation 先事务式校验全部 commits，再构造可调用 Conversation。
func (instance *Provider) RestoreConversation(commits []provider.NativeCommitEnvelope) (provider.Conversation, error) {
	turns := make([]nativeTurn, 0, len(commits))
	for index, commit := range commits {
		turn, err := decodeNativeCommit(commit)
		if err != nil {
			return nil, fmt.Errorf("restore OpenAI native commit %d: %w", index+1, err)
		}
		turns = append(turns, turn.clone())
	}
	return &Conversation{provider: instance, history: nativeHistory{turns: turns, revision: uint64(len(turns))}}, nil
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
	if conversation.pending.Load() {
		conversation.active.Store(false)
		return nil, fault.New(fault.CodeTurnFailed, "conversation has an unfinalized sample")
	}

	userItem := NewUserItem(text)
	projectInstructions, hasProjectInstructions, err := input.ProjectInstructions()
	if err != nil {
		conversation.active.Store(false)
		return nil, fault.Wrap(fault.CodeProviderRequest, "read project instructions failed", err)
	}
	var projectInstructionsPointer *domain.ProjectInstructionsSnapshot
	if hasProjectInstructions {
		projectInstructionsPointer = &projectInstructions
	}
	request, err := compileResponsesRequest(
		conversation.provider.config.Model,
		conversation.history.snapshot(),
		projectInstructionsPointer,
		userItem,
	)
	if err != nil {
		conversation.active.Store(false)
		return nil, fault.Wrap(fault.CodeProviderRequest, "compile provider request failed", err)
	}
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

	reducer := newResponsesStreamReducer()
	stopTransport := func() {
		cancelStream()
		for range stream {
		}
	}
	sendTerminal := func(kind provider.StreamEventKind, prepared *provider.PreparedSample, err error) {
		var event provider.StreamEvent
		var buildErr error
		switch kind {
		case provider.StreamEventCompleted:
			event, buildErr = provider.NewCompletedStreamEvent(prepared)
		case provider.StreamEventCancelled:
			event, buildErr = provider.NewCancelledStreamEvent(err)
		default:
			event, buildErr = provider.NewFailedStreamEvent(err)
		}
		if buildErr != nil {
			event, _ = provider.NewFailedStreamEvent(fault.Wrap(fault.CodeStreamProtocol, "OpenAI terminal event is invalid", buildErr))
		}
		output <- event
	}

	for message := range stream {
		if message.Event != nil {
			result, err := reducer.reduce(*message.Event)
			if err != nil {
				stopTransport()
				sendTerminal(provider.StreamEventFailed, nil, err)
				return
			}
			if result.semantic != nil {
				event, eventErr := provider.NewSemanticStreamEvent(*result.semantic)
				if eventErr != nil {
					stopTransport()
					sendTerminal(provider.StreamEventFailed, nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI semantic event is invalid", eventErr))
					return
				}
				output <- event
			}
			if result.native != nil {
				item := result.native.clone()
				event, eventErr := provider.NewNativeStreamEvent(item)
				if eventErr != nil {
					stopTransport()
					sendTerminal(provider.StreamEventFailed, nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI native event is invalid", eventErr))
					return
				}
				output <- event
			}
			if result.completed {
				stopTransport()
				usage, usageErr := reducer.sampleUsage()
				if usageErr != nil {
					sendTerminal(provider.StreamEventFailed, nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample usage is invalid", usageErr))
					return
				}
				turn := nativeTurn{User: userItem.clone(), Outputs: reducer.outputItems(), Usage: reducer.rawUsage()}
				envelope, encodeErr := encodeNativeCommit(turn)
				if encodeErr != nil {
					sendTerminal(provider.StreamEventFailed, nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample is invalid", encodeErr))
					return
				}
				conversation.pending.Store(true)
				prepared, prepareErr := provider.NewPreparedSample(envelope, usage, func() {
					conversation.history.commit(turn)
					conversation.pending.Store(false)
				})
				if prepareErr != nil {
					conversation.pending.Store(false)
					sendTerminal(provider.StreamEventFailed, nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample cannot be prepared", prepareErr))
					return
				}
				sendTerminal(provider.StreamEventCompleted, prepared, nil)
				return
			}
			continue
		}

		if message.Err != nil {
			kind, terminalErr := mapTransportTerminal(ctx, message.Err)
			sendTerminal(kind, nil, terminalErr)
			return
		}
	}

	if ctx.Err() != nil {
		sendTerminal(provider.StreamEventCancelled, nil, fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled))
		return
	}
	sendTerminal(provider.StreamEventFailed, nil, fault.New(fault.CodeStreamProtocol, "provider stream closed before response.completed"))
}

func mapTransportTerminal(ctx context.Context, err error) (provider.StreamEventKind, error) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return provider.StreamEventCancelled, fault.Wrap(fault.CodeUserCancelled, "turn was cancelled", context.Canceled)
	}
	if transport.IsIdleTimeout(err) {
		return provider.StreamEventFailed, fault.Wrap(fault.CodeStreamIdleTimeout, "provider stream idle timeout", err)
	}
	if errors.Is(err, io.EOF) {
		return provider.StreamEventFailed, fault.New(fault.CodeStreamProtocol, "provider stream closed before response.completed")
	}
	if transport.IsEventTooLarge(err) {
		return provider.StreamEventFailed, fault.Wrap(fault.CodeStreamProtocol, "provider stream event is too large", err)
	}
	return provider.StreamEventFailed, fault.Wrap(fault.CodeStreamProtocol, "provider stream failed", err)
}

func (history *nativeHistory) snapshot() []nativeTurn {
	turns, _ := history.snapshotWithRevision()
	return turns
}

func (history *nativeHistory) snapshotWithRevision() ([]nativeTurn, uint64) {
	history.mu.RLock()
	defer history.mu.RUnlock()
	turns := make([]nativeTurn, 0, len(history.turns))
	for _, turn := range history.turns {
		turns = append(turns, turn.clone())
	}
	return turns, history.revision
}

func (history *nativeHistory) commit(turn nativeTurn) {
	history.mu.Lock()
	defer history.mu.Unlock()
	history.turns = append(history.turns, turn.clone())
	if history.revision < ^uint64(0) {
		history.revision++
	}
}

func (conversation *Conversation) historySnapshot() []nativeTurn {
	return conversation.history.snapshot()
}

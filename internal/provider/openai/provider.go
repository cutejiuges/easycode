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
	"easycode/internal/tool"
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
	entries  []nativeHistoryEntry
	revision uint64
}

var _ provider.Conversation = (*Conversation)(nil)
var _ provider.ToolResultPreparer = (*Conversation)(nil)
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
		FunctionTools:      true,
	}
}

// NewConversation 创建历史相互隔离的会话。
func (instance *Provider) NewConversation() provider.Conversation {
	return &Conversation{provider: instance}
}

// RestoreConversation 先事务式校验全部 commits，再构造可调用 Conversation。
func (instance *Provider) RestoreConversation(commits []provider.NativeCommitEnvelope) (provider.Conversation, error) {
	entries := make([]nativeHistoryEntry, 0, len(commits))
	for index, commit := range commits {
		entry, err := decodeNativeCommit(commit)
		if err != nil {
			return nil, fmt.Errorf("restore OpenAI native commit %d: %w", index+1, err)
		}
		entries = append(entries, entry.clone())
	}
	if err := validateNativeHistory(entries); err != nil {
		return nil, fmt.Errorf("restore OpenAI native history: %w", err)
	}
	return &Conversation{provider: instance, history: nativeHistory{entries: entries, revision: uint64(len(entries))}}, nil
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
	if err := input.Validate(); err != nil {
		return nil, fault.Wrap(fault.CodeTurnFailed, "turn input is invalid", err)
	}
	if !conversation.active.CompareAndSwap(false, true) {
		return nil, fault.New(fault.CodeTurnFailed, "conversation already has an active turn")
	}
	if conversation.pending.Load() {
		conversation.active.Store(false)
		return nil, fault.New(fault.CodeTurnFailed, "conversation has an unfinalized sample")
	}

	expectsContinuation := conversation.history.expectsContinuation()
	if input.IsToolContinuation() && !expectsContinuation {
		conversation.active.Store(false)
		return nil, fault.New(fault.CodeTurnFailed, "turn input does not match native history state")
	}
	var userItem *NativeItem
	if !input.IsToolContinuation() {
		item := NewUserItem(strings.TrimSpace(input.Text))
		userItem = &item
	}
	projectInstructions, hasProjectInstructions, err := input.ProjectInstructions()
	if err != nil {
		conversation.active.Store(false)
		return nil, fault.Wrap(fault.CodeProviderRequest, "read project instructions failed", err)
	}
	var projectInstructionsPointer *domain.ProjectInstructionsSnapshot
	if hasProjectInstructions {
		projectInstructionsPointer = &projectInstructions
	}
	catalog, hasCatalog, err := input.ToolCatalog()
	if err != nil || !hasCatalog {
		conversation.active.Store(false)
		return nil, fault.New(fault.CodeProviderRequest, "tool catalog is required")
	}
	toolView, err := catalog.View(domain.ProviderOpenAI)
	if err != nil {
		conversation.active.Store(false)
		return nil, fault.Wrap(fault.CodeProviderRequest, "compile tool catalog failed", err)
	}
	request, err := compileResponsesRequest(
		conversation.provider.config.Model,
		conversation.history.snapshot(),
		projectInstructionsPointer,
		userItem,
		toolView,
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
	go conversation.consumeStream(ctx, cancelStream, userItem, catalog, stream, output)
	return output, nil
}

func (conversation *Conversation) consumeStream(
	ctx context.Context,
	cancelStream context.CancelFunc,
	userItem *NativeItem,
	catalog tool.CatalogSnapshot,
	stream <-chan transport.SSEMessage,
	output chan<- provider.StreamEvent,
) {
	defer close(output)
	defer conversation.active.Store(false)
	defer cancelStream()

	reducer := newResponsesStreamReducer(catalog)
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
				entry := nativeHistoryEntry{Kind: nativeHistorySample, Outputs: reducer.outputItems(), Usage: reducer.rawUsage()}
				if userItem != nil {
					inputItem := userItem.clone()
					entry.Input = &inputItem
				}
				envelope, encodeErr := encodeNativeCommit(entry)
				if encodeErr != nil {
					sendTerminal(provider.StreamEventFailed, nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample is invalid", encodeErr))
					return
				}
				conversation.pending.Store(true)
				prepared, prepareErr := provider.NewPreparedSampleWithDiscard(envelope, usage, func() {
					conversation.history.commit(entry)
					conversation.pending.Store(false)
				}, func() { conversation.pending.Store(false) }, reducer.readyCalls()...)
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

func (history *nativeHistory) snapshot() []nativeHistoryEntry {
	entries, _ := history.snapshotWithRevision()
	return entries
}

func (history *nativeHistory) snapshotWithRevision() ([]nativeHistoryEntry, uint64) {
	history.mu.RLock()
	defer history.mu.RUnlock()
	entries := make([]nativeHistoryEntry, 0, len(history.entries))
	for _, entry := range history.entries {
		entries = append(entries, entry.clone())
	}
	return entries, history.revision
}

func (history *nativeHistory) commit(entry nativeHistoryEntry) {
	history.mu.Lock()
	defer history.mu.Unlock()
	history.entries = append(history.entries, entry.clone())
	if history.revision < ^uint64(0) {
		history.revision++
	}
}

func (history *nativeHistory) expectsContinuation() bool {
	history.mu.RLock()
	defer history.mu.RUnlock()
	return len(history.entries) > 0 && history.entries[len(history.entries)-1].Kind == nativeHistoryToolOutputs
}

func (conversation *Conversation) historySnapshot() []nativeHistoryEntry {
	return conversation.history.snapshot()
}

// PrepareToolOutputs 在纯内存中编码与最后一组function calls配对的结果。
func (conversation *Conversation) PrepareToolOutputs(results []tool.InvocationResult) (*provider.PreparedToolOutputs, error) {
	if conversation == nil || conversation.active.Load() {
		return nil, fmt.Errorf("OpenAI conversation cannot prepare tool outputs while active")
	}
	if !conversation.pending.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("OpenAI conversation has an unfinalized native entry")
	}
	resetPending := true
	defer func() {
		if resetPending {
			conversation.pending.Store(false)
		}
	}()
	entry, err := prepareToolOutputsEntry(conversation.history.snapshot(), results)
	if err != nil {
		return nil, err
	}
	envelope, err := encodeNativeCommit(entry)
	if err != nil {
		return nil, err
	}
	prepared, err := provider.NewPreparedToolOutputsWithDiscard(envelope, func() {
		conversation.history.commit(entry)
		conversation.pending.Store(false)
	}, func() { conversation.pending.Store(false) })
	if err != nil {
		return nil, err
	}
	resetPending = false
	return prepared, nil
}

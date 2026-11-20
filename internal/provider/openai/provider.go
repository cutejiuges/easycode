// Package openai 实现 OpenAI Responses Provider Kernel。
package openai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"easycode/internal/codec"
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
	CachePolicy       domain.CachePolicy
	Inference         domain.InferenceSettings
}

// Provider 持有不可变 OpenAI 配置和共享 Resty transport。
type Provider struct {
	config    Config
	transport *transport.Client
	websocket *transport.WSClient
}

// Conversation 持有一条会话独占的 OpenAI 原生历史。
type Conversation struct {
	provider     *Provider
	threadID     domain.ThreadID
	baseline     provider.RequestBaseline
	nextReason   domain.RequestResetReason
	socket       *transport.WSConnection
	wsResponseID string
	httpFallback atomic.Bool
	fallback     domain.CacheFallbackReason
	history      nativeHistory
	active       atomic.Bool
	pending      atomic.Bool
}

var _ provider.Conversation = (*Conversation)(nil)
var _ provider.ToolResultPreparer = (*Conversation)(nil)
var _ provider.Factory = (*Provider)(nil)

// New 创建 OpenAI Provider。
func New(config Config) (*Provider, error) {
	config.CachePolicy = domain.WithDefaultCachePolicy(config.CachePolicy)
	if err := config.CachePolicy.ValidateFamily(domain.ProviderOpenAI); err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "cache policy is invalid")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, fault.New(fault.CodeInvalidConfiguration, "OpenAI model is required")
	}
	if config.APIKey.Empty() {
		return nil, fault.New(fault.CodeInvalidConfiguration, "OpenAI API key is required")
	}
	var settingsErr error
	config.Inference, settingsErr = domain.ResolveInferenceSettings(domain.ProviderOpenAI, config.Inference)
	if settingsErr != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "inference configuration is invalid")
	}
	client, err := transport.NewClient(config.BaseURL)
	if err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "OpenAI base URL is invalid", err)
	}
	wsClient, err := transport.NewWSClient(config.BaseURL)
	if err != nil {
		return nil, err
	}
	return &Provider{config: config, transport: client, websocket: wsClient}, nil
}

// Family 返回 OpenAI 协议家族。
func (*Provider) Family() domain.ProviderFamily {
	return domain.ProviderOpenAI
}

// Capabilities 返回当前文本切片已实现的能力集。
func (instance *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		PromptCacheKey:     instance.config.CachePolicy.Key() == domain.OpenAICacheThread,
		PreviousResponse:   instance.config.CachePolicy.Transport() == domain.OpenAIWebSocketAuto,
		Streaming:          true,
		EncryptedReasoning: true,
		ReasoningSummary:   instance.config.Inference.Summary() != "",
		FunctionTools:      true,
		CustomTools:        true,
		FilePatch:          true,
	}
}

// NewConversation 创建历史相互隔离的会话。
func (instance *Provider) NewConversation(threadID domain.ThreadID) (provider.Conversation, error) {
	if !threadID.Valid() {
		return nil, fmt.Errorf("conversation thread ID is invalid")
	}
	return &Conversation{provider: instance, threadID: threadID}, nil
}

// RestoreConversation 先事务式校验全部 commits，再构造可调用 Conversation。
func (instance *Provider) RestoreConversation(threadID domain.ThreadID, commits []provider.RestoreRecord) (provider.Conversation, error) {
	if !threadID.Valid() {
		return nil, fmt.Errorf("conversation thread ID is invalid")
	}
	conversation := &Conversation{provider: instance, threadID: threadID, nextReason: domain.ResetRestored}
	if err := restoreNativeHistory(context.Background(), instance.config.Model, commits, &conversation.history); err != nil {
		return nil, err
	}
	return conversation, nil
}

// Close 释放 HTTP transport 资源。
func (instance *Provider) Close() error {
	return errors.Join(instance.websocket.Close(), instance.transport.Close())
}

// Family 返回 OpenAI 协议家族。
func (*Conversation) Family() domain.ProviderFamily {
	return domain.ProviderOpenAI
}

// Capabilities 返回当前会话所属 Provider 的能力集。
func (conversation *Conversation) Capabilities() provider.Capabilities {
	capabilities := conversation.provider.Capabilities()
	capabilities.PreviousResponse = capabilities.PreviousResponse && !conversation.httpFallback.Load()
	return capabilities
}

// Stream 发起一次 Responses 文本 turn。
func (conversation *Conversation) Stream(ctx context.Context, input provider.TurnInput) (<-chan provider.StreamEvent, error) {
	return provider.StartSample(ctx, input, &conversation.active, &conversation.pending, provider.SamplingStrategy[nativeHistoryEntry]{
		History: conversation.samplingHistory,
		Plan:    conversation.openSamplingPlan,
		Errors:  samplingErrors(),
	})
}

func samplingErrors() provider.SampleStreamErrors {
	return provider.SampleStreamErrors{
		ProviderLabel: "OpenAI", MissingTerminal: "provider stream closed before response.completed",
	}
}

func (conversation *Conversation) samplingHistory() ([]nativeHistoryEntry, bool) {
	history := conversation.history.snapshot()
	if len(history) == 0 || validateNativeHistory(history) != nil {
		return nil, false
	}
	kind := history[len(history)-1].Kind
	return history, kind == nativeHistoryUserInputs || kind == nativeHistoryToolOutputs || kind == nativeHistoryHookFeedback
}

func (conversation *Conversation) openSamplingPlan(history []nativeHistoryEntry, input provider.TurnInput, catalog tool.CatalogSnapshot) (func(context.Context) (provider.SampleStream, error), error) {
	plan, err := conversation.planSamplingRequest(history, input)
	if err != nil {
		return nil, err
	}
	request, err := plan.body()
	if err != nil {
		return nil, fault.Wrap(fault.CodeProviderRequest, "compile provider request failed", err)
	}
	evidence, err := plan.evidence()
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (provider.SampleStream, error) {
		stream, selection, cleanup, err := conversation.startRequest(ctx, request, evidence, plan.purpose)
		if err != nil {
			return provider.SampleStream{}, err
		}
		return conversation.sampleStream(catalog, stream, request, evidence, selection, cleanup), nil
	}, nil
}

func (conversation *Conversation) sampleStream(catalog tool.CatalogSnapshot, messages <-chan transport.SSEMessage, request codec.CanonicalJSON, evidence provider.RequestEvidence, selection requestSelection, cleanup func()) provider.SampleStream {
	reducer := newResponsesStreamReducer(catalog)
	return provider.SampleStream{
		Messages: messages,
		Reduce: func(event transport.SSEEvent) (provider.SampleReduction, error) {
			if event.Name == "error" {
				return provider.SampleReduction{}, fault.New(fault.CodeProviderRequest, "provider reported a websocket error")
			}
			result, err := reducer.reduce(event)
			if err != nil {
				return provider.SampleReduction{}, err
			}
			reduced := provider.SampleReduction{Semantic: result.semantic, Reasoning: result.reasoning, Complete: result.completed}
			if result.native != nil {
				reduced.Native = result.native.clone()
			}
			return reduced, nil
		},
		Cleanup: func(success bool) {
			cleanup()
			if !success {
				conversation.closeWebsocket()
			}
		},
		Prepare: func() (*provider.PreparedSample, error) {
			usage, err := reducer.sampleUsage()
			if err != nil {
				return nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample usage is invalid", err)
			}
			entry := nativeHistoryEntry{Kind: nativeHistorySample, Outputs: reducer.outputItems(), Usage: reducer.rawUsage()}
			snapshot := conversation.history.snapshot()
			entry.TurnID = snapshot[len(snapshot)-1].TurnID
			envelope, err := encodeNativeCommit(entry)
			if err != nil {
				return nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample is invalid", err)
			}
			receipt, err := conversation.describeRequest(request, evidence, domain.RequestSample)
			if err == nil {
				receipt, err = selection.apply(receipt)
			}
			if err != nil {
				return nil, err
			}
			baseline, err := provider.BaselineWithOutput(evidence, entry.Outputs)
			if err != nil {
				return nil, err
			}
			presentation, err := reducer.presentation()
			if err != nil {
				return nil, err
			}
			conversation.pending.Store(true)
			prepared, err := provider.NewPreparedSample(envelope, usage, receipt, presentation, func() {
				conversation.baseline = baseline
				if selection.transport == domain.TransportWebSocket && conversation.socket != nil && !conversation.socket.Closed() {
					conversation.wsResponseID = reducer.responseIdentity()
				}
				conversation.nextReason = domain.ResetNone
				conversation.history.commit(entry)
				conversation.pending.Store(false)
			}, func() { conversation.pending.Store(false); conversation.closeWebsocket() }, reducer.readyCalls()...)
			if err != nil {
				conversation.pending.Store(false)
				return nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample cannot be prepared", err)
			}
			return prepared, nil
		},
	}
}

// PrepareToolOutputs 在纯内存中编码与最后一组function calls配对的结果。
func (conversation *Conversation) PrepareToolOutputs(results []tool.ModelOutput) (*provider.PreparedToolOutputs, error) {
	if conversation == nil {
		return nil, fmt.Errorf("OpenAI conversation cannot prepare tool outputs while active")
	}
	return provider.PrepareNativeToolOutputs(&conversation.history.NativeHistory, &conversation.active, &conversation.pending,
		"OpenAI conversation cannot prepare tool outputs while active", "OpenAI conversation has an unfinalized native entry",
		results, prepareToolOutputsEntry, encodeNativeCommit)
}

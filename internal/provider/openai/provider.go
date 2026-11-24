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
	// 每个终结采样发布独立地址，区分同一回合内的多个样本。
	planReviewTurn atomic.Pointer[domain.TurnID]
	fallback       domain.CacheFallbackReason
	history        nativeHistory
	operations     provider.ConversationOperations
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
func (instance *Provider) RestoreConversation(ctx context.Context, threadID domain.ThreadID, commits []provider.RestoreRecord) (provider.Conversation, provider.CheckpointReader, error) {
	if !threadID.Valid() {
		return nil, nil, fmt.Errorf("conversation thread ID is invalid")
	}
	conversation := &Conversation{provider: instance, threadID: threadID, nextReason: domain.ResetRestored}
	checkpoints, err := restoreNativeHistory(ctx, instance.config.Model, commits, &conversation.history)
	if err != nil {
		return nil, nil, err
	}
	return conversation, checkpoints, nil
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
func (conversation *Conversation) Stream(ctx context.Context, input provider.TurnInput) (provider.StreamEvents, error) {
	return provider.StartSample(ctx, input, &conversation.operations, provider.SamplingStrategy[nativeWindowSnapshot]{
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

func (conversation *Conversation) samplingHistory() (nativeWindowSnapshot, bool) {
	window := conversation.history.Snapshot()
	if len(window.Entries) == 0 || validateNativeHistory(window.Entries) != nil {
		return nativeWindowSnapshot{}, false
	}
	last, ok := provider.LastNativeConversationEntry(window.Entries)
	return window, ok && (last.Kind == nativeHistoryUserInputs || last.Kind == nativeHistoryToolOutputs || last.Kind == nativeHistoryHookFeedback)
}

func (conversation *Conversation) openSamplingPlan(window nativeWindowSnapshot, input provider.TurnInput, catalog tool.CatalogSnapshot) (func(context.Context) (provider.SampleStream, error), error) {
	last, ok := provider.LastNativeConversationEntry(window.Entries)
	if !ok {
		return nil, fmt.Errorf("sampling requires a committed conversation predecessor")
	}
	plan, err := conversation.planSamplingRequest(window, input)
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
	comparison := evidence.Compare(conversation.baseline)
	return func(ctx context.Context) (provider.SampleStream, error) {
		stream, selection, cleanup, err := conversation.startRequest(ctx, plan, request, comparison)
		if err != nil {
			return provider.SampleStream{}, err
		}
		return conversation.sampleStream(last.TurnID, catalog, stream, request, evidence, comparison, selection, cleanup), nil
	}, nil
}

func (conversation *Conversation) sampleStream(turn domain.TurnID, catalog tool.CatalogSnapshot, messages <-chan transport.SSEMessage, request codec.CanonicalJSON, evidence provider.RequestEvidence, comparison provider.RequestComparison, selection requestSelection, cleanup func()) provider.SampleStream {
	reducer := newResponsesStreamReducer(catalog)
	return provider.SampleStream{
		Messages:     messages,
		ValidateTail: selection.transport == domain.TransportHTTP,
		Reduce: func(event transport.SSEEvent) (provider.SampleReduction, error) {
			result, err := reducer.reduce(event)
			if err != nil {
				return provider.SampleReduction{}, err
			}
			return provider.SampleReduction{Semantic: result.semantic, Reasoning: result.reasoning, Complete: result.completed}, nil
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
			entry.TurnID = turn
			envelope, err := encodeNativeCommit(entry)
			if err != nil {
				return nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample is invalid", err)
			}
			receipt, err := conversation.describeRequest(request, evidence, comparison, domain.RequestSample)
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
			prepared, err := provider.NewPreparedSample(envelope, usage, receipt, presentation, func() {
				conversation.baseline = baseline
				if selection.transport == domain.TransportWebSocket && conversation.socket != nil && !conversation.socket.Closed() {
					conversation.wsResponseID = reducer.responseIdentity()
				}
				conversation.nextReason = domain.ResetNone
				conversation.planReviewTurn.Store(nil)
				conversation.history.Append(entry)
				if reducer.planReviewCompleted {
					reviewTurn := entry.TurnID
					conversation.planReviewTurn.Store(&reviewTurn)
				}
				conversation.operations.ResolvePrepared()
			}, func() {
				conversation.closeWebsocket()
				conversation.operations.ResolvePrepared()
			}, reducer.readyCalls()...)
			if err != nil {
				return nil, fault.Wrap(fault.CodeStreamProtocol, "OpenAI completed sample cannot be prepared", err)
			}
			conversation.operations.HoldPrepared()
			return prepared, nil
		},
	}
}

// PrepareToolOutputs 在纯内存中编码与最后一组function calls配对的结果。
func (conversation *Conversation) PrepareToolOutputs(results []tool.ModelOutput) (*provider.PreparedToolOutputs, error) {
	if conversation == nil {
		return nil, fmt.Errorf("OpenAI conversation cannot prepare tool outputs while active")
	}
	return provider.PrepareNativeToolOutputs(&conversation.history, &conversation.operations,
		"OpenAI conversation cannot prepare tool outputs while active", "OpenAI conversation has an unfinalized native entry",
		results, prepareToolOutputsEntry, encodeNativeCommit)
}

// Package anthropic 实现 Anthropic Messages Provider Kernel。
package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

const (
	// DefaultMaxOutputTokens 是固定请求基线采用的缺省值。
	DefaultMaxOutputTokens = domain.DefaultAnthropicOutputTokens
	apiVersion             = "2023-06-01"
)

// Config 描述 Anthropic Messages 连接信息。
type Config struct {
	BaseURL           string
	APIKey            secret.Value
	Model             string
	Inference         domain.InferenceSettings
	StreamIdleTimeout time.Duration
	CachePolicy       domain.CachePolicy
}

// Provider 持有不可变 Anthropic 配置和共享 Resty transport。
type Provider struct {
	config    Config
	transport *transport.Client
}

// Conversation 持有一条会话独占的 Anthropic 原生历史。
type Conversation struct {
	provider   *Provider
	threadID   domain.ThreadID
	baseline   provider.RequestBaseline
	nextReason domain.RequestResetReason
	history    nativeHistory
	operations provider.ConversationOperations
}

var _ provider.Conversation = (*Conversation)(nil)
var _ provider.ToolResultPreparer = (*Conversation)(nil)
var _ provider.Factory = (*Provider)(nil)

// New 创建 Anthropic Provider。
func New(config Config) (*Provider, error) {
	config.CachePolicy = domain.WithDefaultCachePolicy(config.CachePolicy)
	if err := config.CachePolicy.ValidateFamily(domain.ProviderAnthropic); err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "cache policy is invalid")
	}
	config.Model = strings.TrimSpace(config.Model)
	if config.Model == "" {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Anthropic model is required")
	}
	if strings.TrimSpace(config.APIKey.Reveal()) == "" {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Anthropic API key is required")
	}
	var settingsErr error
	config.Inference, settingsErr = domain.ResolveInferenceSettings(domain.ProviderAnthropic, config.Inference)
	if settingsErr != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "inference configuration is invalid")
	}
	client, err := transport.NewClient(config.BaseURL)
	if err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "Anthropic base URL is invalid", err)
	}
	return &Provider{config: config, transport: client}, nil
}

// Family 返回 Anthropic 协议家族。
func (*Provider) Family() domain.ProviderFamily {
	return domain.ProviderAnthropic
}

// Capabilities 返回当前文本切片已实现的能力集。
func (instance *Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		PromptCacheControl: instance.config.CachePolicy.Anthropic() != domain.AnthropicCacheDisabled,
		Streaming:          true,
		ThinkingSignature:  true,
		FunctionTools:      true,
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
	return instance.transport.Close()
}

// Family 返回 Anthropic 协议家族。
func (*Conversation) Family() domain.ProviderFamily {
	return domain.ProviderAnthropic
}

// Capabilities 返回当前会话所属 Provider 的能力集。
func (conversation *Conversation) Capabilities() provider.Capabilities {
	return conversation.provider.Capabilities()
}

// Stream 发起一次 Messages 文本 turn。
func (conversation *Conversation) Stream(ctx context.Context, input provider.TurnInput) (provider.StreamEvents, error) {
	return provider.StartSample(ctx, input, &conversation.operations, provider.SamplingStrategy[nativeWindowSnapshot]{
		History: conversation.samplingHistory,
		Plan:    conversation.openSamplingPlan,
		Errors:  samplingErrors(),
	})
}

func samplingErrors() provider.SampleStreamErrors {
	return provider.SampleStreamErrors{
		ProviderLabel: "Anthropic", MissingTerminal: "provider stream closed before message_stop",
		CancelOpenWithParent: true,
	}
}

func (conversation *Conversation) samplingHistory() (nativeWindowSnapshot, bool) {
	window := conversation.history.Snapshot()
	if len(window.Entries) == 0 || validateNativeHistory(window.Entries) != nil {
		return nativeWindowSnapshot{}, false
	}
	kind := window.Entries[len(window.Entries)-1].Kind
	return window, kind == nativeHistoryUserInputs || kind == nativeHistoryToolOutputs || kind == nativeHistoryHookFeedback
}

func (conversation *Conversation) openSamplingPlan(window nativeWindowSnapshot, input provider.TurnInput, catalog tool.CatalogSnapshot) (func(context.Context) (provider.SampleStream, error), error) {
	plan, err := conversation.planSamplingRequest(window, input)
	if err != nil {
		return nil, err
	}
	request, err := plan.compile(conversation.provider.config.CachePolicy.Anthropic())
	if err != nil {
		return nil, fault.Wrap(fault.CodeProviderRequest, "compile provider request failed", err)
	}
	evidence, err := plan.evidence()
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) (provider.SampleStream, error) {
		stream, err := conversation.startRequest(ctx, request.body)
		if err != nil {
			return provider.SampleStream{}, err
		}
		return conversation.sampleStream(window.Entries[len(window.Entries)-1].TurnID, catalog, stream, request, evidence), nil
	}, nil
}

func (conversation *Conversation) sampleStream(turn domain.TurnID, catalog tool.CatalogSnapshot, messages <-chan transport.SSEMessage, request compiledMessagesRequest, evidence provider.RequestEvidence) provider.SampleStream {
	reducer := newStreamReducer(catalog)
	return provider.SampleStream{
		Messages: messages,
		Reduce: func(event transport.SSEEvent) (provider.SampleReduction, error) {
			result, err := reducer.reduce(event)
			if err != nil {
				return provider.SampleReduction{}, err
			}
			return provider.SampleReduction{Semantic: result.semantic, Reasoning: result.reasoning, Complete: result.complete}, nil
		},
		Prepare: func() (*provider.PreparedSample, error) {
			usage, err := reducer.sampleUsage()
			if err != nil {
				return nil, fault.Wrap(fault.CodeStreamProtocol, "Anthropic completed sample usage is invalid", err)
			}
			entry := nativeHistoryEntry{Kind: nativeHistorySample, Assistant: reducer.assistantMessage(), Metadata: reducer.messageMetadata()}
			entry.TurnID = turn
			envelope, err := encodeNativeCommit(entry)
			if err != nil {
				return nil, fault.Wrap(fault.CodeStreamProtocol, "Anthropic completed sample is invalid", err)
			}
			receipt, err := conversation.describeRequest(request, evidence, domain.RequestSample)
			if err != nil {
				return nil, err
			}
			baseline, err := provider.BaselineWithOutput(evidence, []nativeMessage{entry.Assistant})
			if err != nil {
				return nil, err
			}
			presentation, err := reducer.presentation()
			if err != nil {
				return nil, err
			}
			prepared, err := provider.NewPreparedSample(envelope, usage, receipt, presentation, func() {
				conversation.baseline = baseline
				conversation.nextReason = domain.ResetNone
				conversation.history.Append(entry)
				conversation.operations.ResolvePrepared()
			}, conversation.operations.ResolvePrepared, reducer.readyCalls()...)
			if err != nil {
				return nil, fault.Wrap(fault.CodeStreamProtocol, "Anthropic completed sample cannot be prepared", err)
			}
			conversation.operations.HoldPrepared()
			return prepared, nil
		},
	}
}

func (conversation *Conversation) startRequest(ctx context.Context, body codec.CanonicalJSON) (<-chan transport.SSEMessage, error) {
	return conversation.provider.transport.StreamSSE(ctx, transport.SSERequest{
		Method: http.MethodPost,
		Path:   "messages",
		Headers: map[string]string{
			"x-api-key":         conversation.provider.config.APIKey.Reveal(),
			"anthropic-version": apiVersion,
			"Accept":            "text/event-stream",
			"Content-Type":      "application/json",
		},
		Body: body,
	}, transport.StreamOptions{IdleTimeout: conversation.provider.config.StreamIdleTimeout})
}

// PrepareToolOutputs 在纯内存中编码与最后一组tool_use配对的结果。
func (conversation *Conversation) PrepareToolOutputs(results []tool.ModelOutput) (*provider.PreparedToolOutputs, error) {
	if conversation == nil {
		return nil, fmt.Errorf("anthropic conversation cannot prepare tool outputs while active")
	}
	return provider.PrepareNativeToolOutputs(&conversation.history, &conversation.operations,
		"anthropic conversation cannot prepare tool outputs while active", "anthropic conversation has an unfinalized native entry",
		results, prepareToolOutputsEntry, encodeNativeCommit)
}

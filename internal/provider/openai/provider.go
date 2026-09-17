// Package openai 实现 OpenAI Responses Provider Kernel。
package openai

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/secret"
)

// Config 描述 OpenAI Responses 连接信息。
type Config struct {
	BaseURL string
	APIKey  secret.Value
	Model   string
}

// Provider 持有 OpenAI 原生历史和 Resty transport 的实现边界。
type Provider struct {
	config    Config
	transport *transport.Client
}

// New 创建 OpenAI Provider。
func New(config Config) *Provider {
	return &Provider{
		config:    config,
		transport: transport.NewClient(config.BaseURL),
	}
}

// Family 返回 OpenAI 协议家族。
func (*Provider) Family() domain.ProviderFamily {
	return domain.ProviderOpenAI
}

// Capabilities 返回 Responses wire 的默认能力集。
func (*Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Streaming:          true,
		ReasoningSummary:   true,
		RawReasoning:       true,
		EncryptedReasoning: true,
		FunctionTools:      true,
		CustomTools:        true,
		ParallelToolCalls:  true,
		PromptCacheKey:     true,
		PreviousResponse:   true,
	}
}

// Stream 将在 P1 阶段实现 Responses request compiler 和 response-item reducer。
func (*Provider) Stream(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
	return nil, fault.New(fault.CodeNotImplemented, "OpenAI streaming is not implemented")
}

// Close 释放 HTTP transport 资源。
func (instance *Provider) Close() error {
	return instance.transport.Close()
}

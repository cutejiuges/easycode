// Package anthropic 实现 Anthropic Messages Provider Kernel。
package anthropic

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/secret"
)

// Config 描述 Anthropic Messages 连接信息。
type Config struct {
	BaseURL string
	APIKey  secret.Value
	Model   string
}

// Provider 持有 Anthropic 原生历史和 Resty transport 的实现边界。
type Provider struct {
	config    Config
	transport *transport.Client
}

// New 创建 Anthropic Provider。
func New(config Config) *Provider {
	return &Provider{
		config:    config,
		transport: transport.NewClient(config.BaseURL),
	}
}

// Family 返回 Anthropic 协议家族。
func (*Provider) Family() domain.ProviderFamily {
	return domain.ProviderAnthropic
}

// Capabilities 返回 Messages wire 的默认能力集。
func (*Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Streaming:          true,
		ThinkingSignature:  true,
		FunctionTools:      true,
		ParallelToolCalls:  true,
		PromptCacheControl: true,
	}
}

// Stream 将在 P1 阶段实现 Anthropic request compiler 和 content-block reducer。
func (*Provider) Stream(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
	return nil, fault.New(fault.CodeNotImplemented, "Anthropic streaming is not implemented")
}

// Close 释放 HTTP transport 资源。
func (instance *Provider) Close() error {
	return instance.transport.Close()
}

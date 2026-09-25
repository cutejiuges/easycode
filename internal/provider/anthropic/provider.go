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
func New(config Config) (*Provider, error) {
	client, err := transport.NewClient(config.BaseURL)
	if err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "Anthropic base URL is invalid", err)
	}
	return &Provider{
		config:    config,
		transport: client,
	}, nil
}

// Family 返回 Anthropic 协议家族。
func (*Provider) Family() domain.ProviderFamily {
	return domain.ProviderAnthropic
}

// Capabilities 返回当前占位实现实际具备的能力集。
func (*Provider) Capabilities() provider.Capabilities {
	return provider.Capabilities{}
}

// NewConversation 创建相互隔离的 Anthropic 会话占位实现。
func (instance *Provider) NewConversation() provider.Conversation {
	return &Conversation{provider: instance}
}

// Conversation 保存 Anthropic 会话级状态；具体历史将在后续变更实现。
type Conversation struct {
	provider *Provider
}

// Family 返回 Anthropic 协议家族。
func (*Conversation) Family() domain.ProviderFamily {
	return domain.ProviderAnthropic
}

// Capabilities 返回当前 Provider 的能力声明。
func (conversation *Conversation) Capabilities() provider.Capabilities {
	return conversation.provider.Capabilities()
}

// Stream 将在后续变更实现 Anthropic request compiler 和 content-block reducer。
func (*Conversation) Stream(context.Context, provider.TurnInput) (<-chan provider.StreamEvent, error) {
	return nil, fault.New(fault.CodeNotImplemented, "Anthropic streaming is not implemented")
}

// Close 释放 HTTP transport 资源。
func (instance *Provider) Close() error {
	return instance.transport.Close()
}

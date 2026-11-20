package app

import (
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
	"easycode/internal/provider/anthropic"
	"easycode/internal/provider/openai"
)

type providerResource interface {
	provider.Factory
	Close() error
}

// providerBinding 以同一协议身份选择在线工厂和无凭据的只读策略。
type providerBinding struct {
	wire    string
	open    func(config.Provider, config.ProviderSettings) (providerResource, error)
	history provider.StoredHistoryReader
}

func bindProvider(family domain.ProviderFamily) (providerBinding, error) {
	wire := family.Wire()
	switch family {
	case domain.ProviderAnthropic:
		return providerBinding{wire: wire, open: openAnthropic, history: anthropic.NewHistoryReader()}, nil
	case domain.ProviderOpenAI:
		return providerBinding{wire: wire, open: openOpenAI, history: openai.NewHistoryReader()}, nil
	default:
		return providerBinding{}, fault.New(fault.CodeProviderUnavailable, "provider is unavailable")
	}
}

func openAnthropic(target config.Provider, settings config.ProviderSettings) (providerResource, error) {
	return anthropic.New(anthropic.Config{
		BaseURL:     target.BaseURL,
		APIKey:      target.APIKey,
		Model:       target.Model,
		CachePolicy: settings.Cache,
		Inference:   settings.Inference,
	})
}

func openOpenAI(target config.Provider, settings config.ProviderSettings) (providerResource, error) {
	instance, err := openai.New(openai.Config{
		BaseURL:     target.BaseURL,
		APIKey:      target.APIKey,
		Model:       target.Model,
		CachePolicy: settings.Cache,
		Inference:   settings.Inference,
	})
	if err == nil && instance.Capabilities().ReasoningSummary != (settings.Inference.Summary() != "") {
		_ = instance.Close()
		return nil, fault.New(fault.CodeInvalidConfiguration, "reasoning capability is inconsistent")
	}
	return instance, err
}

// sessionProvider 仅持有创建与恢复会话需要的工厂和原生证据接口。
type sessionProvider struct {
	factory provider.Factory
	wire    string
	history provider.StoredHistoryReader
}

func (binding providerBinding) bindSession(factory provider.Factory) sessionProvider {
	return sessionProvider{factory: factory, wire: binding.wire, history: binding.history}
}

func bindTranscriptProjector(conversation provider.Conversation) provider.TranscriptProjector {
	projector, _ := conversation.(provider.TranscriptProjector)
	return projector
}

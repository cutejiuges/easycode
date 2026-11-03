package domain

import (
	"crypto/sha256"
	"easycode/internal/codec"
	"encoding/hex"
	"fmt"
)

// AnthropicCacheMode 表示显式 Messages 缓存策略。
type AnthropicCacheMode string

const (
	AnthropicCacheDisabled AnthropicCacheMode = "disabled"
	AnthropicCache5m       AnthropicCacheMode = "ephemeral_5m"
	AnthropicCache1h       AnthropicCacheMode = "ephemeral_1h"
)

// OpenAICacheKeyMode 表示 Responses 缓存身份策略。
type OpenAICacheKeyMode string

const (
	OpenAICacheOmit   OpenAICacheKeyMode = "omit"
	OpenAICacheThread OpenAICacheKeyMode = "thread"
)

// OpenAITransportMode 表示显式允许的请求传输策略。
type OpenAITransportMode string

const (
	OpenAIHTTPSSE       OpenAITransportMode = "http_sse"
	OpenAIWebSocketAuto OpenAITransportMode = "websocket_auto"
)

// CachePolicy 是不含连接凭据的不可变策略。
type CachePolicy struct {
	anthropic AnthropicCacheMode
	key       OpenAICacheKeyMode
	transport OpenAITransportMode
}

// NewCachePolicy 校验全部显式策略，不接受空值或未知枚举。
func NewCachePolicy(anthropic AnthropicCacheMode, key OpenAICacheKeyMode, transport OpenAITransportMode) (CachePolicy, error) {
	p := CachePolicy{anthropic, key, transport}
	if err := p.Validate(); err != nil {
		return CachePolicy{}, err
	}
	return p, nil
}

// DefaultCachePolicy 返回不发送显式缓存字段的 HTTP 策略。
func DefaultCachePolicy() CachePolicy {
	return CachePolicy{AnthropicCacheDisabled, OpenAICacheOmit, OpenAIHTTPSSE}
}

// Anthropic 返回 Messages 策略。
func (p CachePolicy) Anthropic() AnthropicCacheMode { return p.anthropic }

// Key 返回 Responses 缓存身份策略。
func (p CachePolicy) Key() OpenAICacheKeyMode { return p.key }

// Transport 返回允许的 Responses 传输策略。
func (p CachePolicy) Transport() OpenAITransportMode { return p.transport }

// Validate 拒绝零值及未知策略。
func (p CachePolicy) Validate() error {
	switch p.anthropic {
	case AnthropicCacheDisabled, AnthropicCache5m, AnthropicCache1h:
	default:
		return fmt.Errorf("invalid Anthropic cache policy")
	}
	switch p.key {
	case OpenAICacheOmit, OpenAICacheThread:
	default:
		return fmt.Errorf("invalid OpenAI cache key policy")
	}
	switch p.transport {
	case OpenAIHTTPSSE, OpenAIWebSocketAuto:
	default:
		return fmt.Errorf("invalid OpenAI transport policy")
	}
	return nil
}

// ValidateFamily 校验最终环境合并后的能力组合。
func (p CachePolicy) ValidateFamily(family ProviderFamily) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if !family.Valid() {
		return fmt.Errorf("invalid provider family")
	}
	if family != ProviderAnthropic && p.anthropic != AnthropicCacheDisabled {
		return fmt.Errorf("Anthropic cache policy requires anthropic provider")
	}
	if family != ProviderOpenAI && (p.key != OpenAICacheOmit || p.transport != OpenAIHTTPSSE) {
		return fmt.Errorf("OpenAI cache and transport policies require openai provider")
	}
	return nil
}

// WithDefaultCachePolicy 仅将未提供配置的零值转换为产品默认值。
func WithDefaultCachePolicy(p CachePolicy) CachePolicy {
	if p == (CachePolicy{}) {
		return DefaultCachePolicy()
	}
	return p
}

// Fingerprint 返回不含真实路由身份的策略指纹。
func (p CachePolicy) Fingerprint() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	canonical, err := codec.MarshalCanonical(struct {
		Anthropic AnthropicCacheMode  `json:"anthropic"`
		Key       OpenAICacheKeyMode  `json:"key"`
		Transport OpenAITransportMode `json:"transport"`
	}{p.anthropic, p.key, p.transport}, 1024)
	if err != nil {
		return "", err
	}
	return ContentFingerprint(canonical.Bytes()), nil
}

// ThreadCacheKey 从固定用途命名空间派生有界路由身份。
func ThreadCacheKey(thread ThreadID) (string, error) {
	if !thread.Valid() {
		return "", fmt.Errorf("cache thread ID is invalid")
	}
	sum := sha256.Sum256([]byte("easycode/prompt-cache/thread\x00" + string(thread)))
	return "easycode:" + hex.EncodeToString(sum[:]), nil
}

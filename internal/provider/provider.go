// Package provider 定义共享 Provider Kernel 契约，不承载具体 wire 结构。
package provider

import (
	"context"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// Capabilities 描述服务端实际支持的能力，不能仅由厂商名称推断。
type Capabilities struct {
	Streaming          bool
	ReasoningSummary   bool
	RawReasoning       bool
	EncryptedReasoning bool
	ThinkingSignature  bool
	FunctionTools      bool
	CustomTools        bool
	ParallelToolCalls  bool
	PromptCacheControl bool
	PromptCacheKey     bool
	PreviousResponse   bool
}

// TurnInput 是共享 turn 模板交给 Provider Kernel 的最小输入。
type TurnInput struct {
	Text string
}

// NativeItem 是 provider 原生 item 的只读领域边界。
// 具体字段必须留在 anthropic/openai 子包的强类型结构中。
type NativeItem interface {
	ProviderFamily() domain.ProviderFamily
	ItemKind() string
}

// StreamEvent 同时携带共享语义事件和可选原生完成项。
type StreamEvent struct {
	Event  protocol.Event
	Native NativeItem
	Err    error
}

// Kernel 是 Runtime 依赖的最小 provider 接口。
type Kernel interface {
	Family() domain.ProviderFamily
	Capabilities() Capabilities
	Stream(context.Context, TurnInput) (<-chan StreamEvent, error)
}

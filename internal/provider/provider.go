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
	Kind   StreamEventKind
	Event  protocol.Event
	Native NativeItem
	Err    error
}

// StreamEventKind 区分普通流事件与恰好一次的终态事件。
type StreamEventKind string

const (
	StreamEventSemantic  StreamEventKind = "semantic"
	StreamEventNative    StreamEventKind = "native_item"
	StreamEventCompleted StreamEventKind = "completed"
	StreamEventFailed    StreamEventKind = "failed"
	StreamEventCancelled StreamEventKind = "cancelled"
)

// Terminal 判断事件是否结束当前 provider stream。
func (kind StreamEventKind) Terminal() bool {
	return kind == StreamEventCompleted || kind == StreamEventFailed || kind == StreamEventCancelled
}

// Conversation 是 Runtime 依赖的会话级 provider 接口。
type Conversation interface {
	Family() domain.ProviderFamily
	Capabilities() Capabilities
	Stream(context.Context, TurnInput) (<-chan StreamEvent, error)
}

// Factory 创建相互隔离的会话级 Provider Conversation。
type Factory interface {
	Family() domain.ProviderFamily
	Capabilities() Capabilities
	NewConversation() Conversation
}

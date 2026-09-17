// Package domain 定义不依赖 I/O 和框架的核心领域类型。
package domain

// SessionID 标识一棵完整会话线程树。
type SessionID string

// ThreadID 标识根线程或 Subagent 线程。
type ThreadID string

// TurnID 标识一次用户输入到最终结束的 turn。
type TurnID string

// ItemID 标识模型原生 item 或共享语义 item。
type ItemID string

// CallID 标识一次工具调用，并作为副作用幂等键。
type CallID string

// ProviderFamily 标识 provider 协议家族，而不是具体厂商名称。
type ProviderFamily string

const (
	ProviderAnthropic ProviderFamily = "anthropic"
	ProviderOpenAI    ProviderFamily = "openai"
)

// Valid 判断 provider 家族是否受支持。
func (family ProviderFamily) Valid() bool {
	return family == ProviderAnthropic || family == ProviderOpenAI
}

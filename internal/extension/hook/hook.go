// Package hook 定义可阻断、改写和补充上下文的 Hook 边界。
package hook

import (
	"context"
	"encoding/json"
)

// TODO(P6): Hook 契约仅为扩展系统阶段保留，当前不得注册或执行；由后续 OpenSpec 同时提供真实消费者、验证与测试时启用，否则删除。

// EventName 是与 Claude 兼容的 Hook 事件名称。
type EventName string

const (
	EventPreToolUse       EventName = "PreToolUse"
	EventPostToolUse      EventName = "PostToolUse"
	EventUserPromptSubmit EventName = "UserPromptSubmit"
	EventSessionStart     EventName = "SessionStart"
	EventSessionEnd       EventName = "SessionEnd"
	EventPreCompact       EventName = "PreCompact"
	EventPostCompact      EventName = "PostCompact"
)

// Request 是传递给 Hook handler 的强类型信封。
type Request struct {
	Event   EventName
	Matcher string
	Payload json.RawMessage
}

// Outcome 描述 Hook 对当前流程的影响。
type Outcome struct {
	Continue          bool
	Reason            string
	AdditionalContext string
	UpdatedPayload    json.RawMessage
}

// Handler 执行一个受信任的 Hook。
type Handler interface {
	Run(context.Context, Request) (Outcome, error)
}

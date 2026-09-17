// Package hook 定义可阻断、改写和补充上下文的 Hook 边界。
package hook

import (
	"context"
	"encoding/json"
)

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

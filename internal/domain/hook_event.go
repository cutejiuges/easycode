package domain

// HookEvent 区分两个具有真实消费者的同步工具检查阶段。
type HookEvent string

const (
	HookPreToolUse  HookEvent = "PreToolUse"
	HookPostToolUse HookEvent = "PostToolUse"
)

func (e HookEvent) Valid() bool { return e == HookPreToolUse || e == HookPostToolUse }

// HookFeedbackPolicy 是独立于命令信任的显式模型披露选择。
type HookFeedbackPolicy string

const (
	HookFeedbackNone  HookFeedbackPolicy = "none"
	HookFeedbackModel HookFeedbackPolicy = "model"
)

func (p HookFeedbackPolicy) Valid() bool { return p == HookFeedbackNone || p == HookFeedbackModel }

func (p HookFeedbackPolicy) ValidFor(e HookEvent) bool {
	return e.Valid() && p.Valid() && (e == HookPostToolUse || p == HookFeedbackNone)
}

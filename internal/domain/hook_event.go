package domain

// HookEvent 区分前置、成功后置及失败后置三个同步检查事件。
type HookEvent string

const (
	HookPreToolUse         HookEvent = "PreToolUse"
	HookPostToolUse        HookEvent = "PostToolUse"
	HookPostToolUseFailure HookEvent = "PostToolUseFailure"
)

func (e HookEvent) Valid() bool { return e == HookPreToolUse || e.Post() }

// Post 表示具有独立反馈而不能阻断原结果的后置事件。
func (e HookEvent) Post() bool { return e == HookPostToolUse || e == HookPostToolUseFailure }

// HookFeedbackPolicy 是独立于命令信任的显式模型披露选择。
type HookFeedbackPolicy string

const (
	HookFeedbackNone  HookFeedbackPolicy = "none"
	HookFeedbackModel HookFeedbackPolicy = "model"
)

func (p HookFeedbackPolicy) Valid() bool { return p == HookFeedbackNone || p == HookFeedbackModel }

func (p HookFeedbackPolicy) ValidFor(e HookEvent) bool {
	return e.Valid() && p.Valid() && (e.Post() || p == HookFeedbackNone)
}

// PostHookRoute 仅表达由原工具事实决定的后置选择，不表示工具是否产生副作用。
type PostHookRoute string

const (
	PostHookRouteSuccess PostHookRoute = "success"
	PostHookRouteFailure PostHookRoute = "failure"
	PostHookRouteNone    PostHookRoute = "none"
)

func (r PostHookRoute) Valid() bool {
	return r == PostHookRouteSuccess || r == PostHookRouteFailure || r == PostHookRouteNone
}

// Event 仅对选中事件返回 true；none 不伪造事件。
func (r PostHookRoute) Event() (HookEvent, bool) {
	switch r {
	case PostHookRouteSuccess:
		return HookPostToolUse, true
	case PostHookRouteFailure:
		return HookPostToolUseFailure, true
	default:
		return "", false
	}
}

package context

import "easycode/internal/domain"

// AutoCompactionDecision 是自动压缩调度的封闭结果，不表示请求已获准。
type AutoCompactionDecision string

const (
	AutoCompactionDisabled       AutoCompactionDecision = "disabled"
	AutoCompactionUnknown        AutoCompactionDecision = "unknown"
	AutoCompactionBelowThreshold AutoCompactionDecision = "below_threshold"
	AutoCompactionEligible       AutoCompactionDecision = "eligible"
	AutoCompactionSuppressed     AutoCompactionDecision = "suppressed"
)

// AutoCompactionPolicy 保存显式预算派生的纯内存调度策略。
type AutoCompactionPolicy struct {
	enabled           bool
	threshold         uint64
	unavailableReason string
}

// NewAutoCompactionPolicy 创建固定策略；小窗口不可用时不影响普通预算。
func NewAutoCompactionPolicy(enabled bool, budget Budget, summaryOutput uint64) (AutoCompactionPolicy, error) {
	if err := budget.Validate(); err != nil {
		return AutoCompactionPolicy{}, err
	}
	if !enabled {
		return AutoCompactionPolicy{unavailableReason: "disabled"}, nil
	}
	if !budget.Enabled() {
		return AutoCompactionPolicy{unavailableReason: "window_not_configured"}, nil
	}
	reserve := max(budget.ReservedOutput(), summaryOutput)
	available := budget.Window() - budget.SafetyMargin()
	if reserve >= available {
		return AutoCompactionPolicy{unavailableReason: "insufficient_summary_reserve"}, nil
	}
	available -= reserve
	return AutoCompactionPolicy{enabled: true, threshold: available/100*80 + available%100*80/100}, nil
}

// Threshold 返回软触发阈值与策略是否可用，不承诺服务端容量。
func (policy AutoCompactionPolicy) Threshold() (uint64, bool) {
	return policy.threshold, policy.enabled
}

// Decide 只根据当前估算与 owner 提供的抑制状态决策。
func (policy AutoCompactionPolicy) Decide(estimate domain.TokenEstimate, suppressed bool) (AutoCompactionDecision, error) {
	if err := estimate.Validate(); err != nil {
		return "", err
	}
	if !policy.enabled {
		return AutoCompactionDisabled, nil
	}
	if suppressed {
		return AutoCompactionSuppressed, nil
	}
	tokens, known := estimate.Tokens()
	if !known {
		return AutoCompactionUnknown, nil
	}
	if tokens < policy.threshold {
		return AutoCompactionBelowThreshold, nil
	}
	return AutoCompactionEligible, nil
}

// UnavailableReason 返回不可用原因，零值明确表示宿主关闭策略。
func (policy AutoCompactionPolicy) UnavailableReason() string {
	if policy.enabled {
		return ""
	}
	if policy.unavailableReason == "" {
		return "disabled"
	}
	return policy.unavailableReason
}

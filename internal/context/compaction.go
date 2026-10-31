package context

import (
	"fmt"

	"easycode/internal/domain"
)

// CompactionRejection 是纯规划阶段的封闭拒绝原因。
type CompactionRejection string

const (
	CompactionBudgetUnknown  CompactionRejection = "compaction_budget_unknown"
	CompactionBudgetExceeded CompactionRejection = "compaction_budget_exceeded"
	CompactionNoReduction    CompactionRejection = "compaction_no_reduction"
)

// CompactionBudgetError 不携带摘要或 prompt 正文。
type CompactionBudgetError struct{ reason CompactionRejection }

// Reason 返回可供 Runtime 映射的强类型原因。
func (err *CompactionBudgetError) Reason() CompactionRejection { return err.reason }

// Error 返回固定英文诊断，不包含请求正文。
func (err *CompactionBudgetError) Error() string { return string(err.reason) }

// CheckCompactionRequest 在网络请求前验证完整摘要请求估算及真实输出预留。
// request 必须由 Provider 对包含摘要指示、工具、项目指令和有效原生窗口的请求估算。
func CheckCompactionRequest(request domain.TokenEstimate, outputTokens uint64, budget Budget) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := budget.Validate(); err != nil {
		return err
	}
	if outputTokens == 0 || outputTokens > 65536 {
		return fmt.Errorf("compaction output tokens are invalid")
	}
	if !budget.Enabled() {
		return nil
	}
	tokens, known := request.Tokens()
	if !known {
		return &CompactionBudgetError{reason: CompactionBudgetUnknown}
	}
	// 按差值逐项扣除，避免输入、输出、安全量求和溢出。
	available := budget.Window() - budget.SafetyMargin()
	if outputTokens >= available || tokens > available-outputTokens {
		return &CompactionBudgetError{reason: CompactionBudgetExceeded}
	}
	return nil
}

// CheckCompactionCandidate 只允许完整可计量且确实缩减的候选进入 durable 提交。
func CheckCompactionCandidate(before, candidate ContextPlan) error {
	if err := before.Validate(); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	oldTokens, oldKnown := before.TotalEstimate().Tokens()
	newTokens, newKnown := candidate.TotalEstimate().Tokens()
	if !oldKnown || !newKnown || before.TotalEstimate().Method() != candidate.TotalEstimate().Method() {
		return &CompactionBudgetError{reason: CompactionBudgetUnknown}
	}
	if newTokens >= oldTokens {
		return &CompactionBudgetError{reason: CompactionNoReduction}
	}
	if candidate.Decision().State() == BudgetOverLimit {
		return &CompactionBudgetError{reason: CompactionBudgetExceeded}
	}
	return nil
}

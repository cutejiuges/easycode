package tool

import "errors"

const (
	// SamplePreviewBytes 和 TurnPreviewBytes 只计最终模型预览，不包括 Provider JSON 包装。
	SamplePreviewBytes = 512 << 10
	TurnPreviewBytes   = 2 << 20
	// SafetyResultBytes 为每个尚可准入的调用保留确定终态空间。
	SafetyResultBytes = 1 << 10
	MaxBudgetCalls    = 64
)

// BudgetReason 记录结果最终处理原因，不用于选择历史版本。
type BudgetReason string

const (
	BudgetPreserved BudgetReason = "preserved"
	BudgetShortened BudgetReason = "shortened"
	BudgetRejected  BudgetReason = "rejected"
	BudgetRecovery  BudgetReason = "recovery"
)

// ResultBudgetMetadata 是随最终结果冻结的预算事实。
type ResultBudgetMetadata struct {
	allowance int
	actual    int
	reason    BudgetReason
}

// NewResultBudgetMetadata 构造不含正文的预算事实。
func NewResultBudgetMetadata(allowance, actual int, reason BudgetReason) (ResultBudgetMetadata, error) {
	v := ResultBudgetMetadata{allowance: allowance, actual: actual, reason: reason}
	return v, v.Validate()
}
func (v ResultBudgetMetadata) Allowance() int       { return v.allowance }
func (v ResultBudgetMetadata) ActualBytes() int     { return v.actual }
func (v ResultBudgetMetadata) Reason() BudgetReason { return v.reason }

// Validate 检查当前额度及封闭原因，零值不是合法事实。
func (v ResultBudgetMetadata) Validate() error {
	if v.allowance < SafetyResultBytes || v.allowance > MaxModelPreviewBytes || v.actual < 0 || v.actual > v.allowance {
		return errors.New("invalid result budget")
	}
	switch v.reason {
	case BudgetPreserved, BudgetShortened, BudgetRejected, BudgetRecovery:
	default:
		return errors.New("invalid result budget reason")
	}
	if (v.reason == BudgetRejected || v.reason == BudgetRecovery) && v.actual > SafetyResultBytes {
		return errors.New("invalid safety result size")
	}
	return nil
}

// ResultBudget 由单一 turn owner 顺序使用，worker 不共享可变账本。
type ResultBudget struct {
	exhausted       bool
	turnUsed        int
	processed       int
	sampleUsed      int
	sampleRemaining int
}

// NewResultBudget 创建纯内存 turn 账本。
func NewResultBudget() *ResultBudget { return &ResultBudget{} }

// BeginSample 在前一调用组已全部冻结后开始下一组，不重置 turn 额度。
func (b *ResultBudget) BeginSample(calls int) error {
	if b == nil || calls < 0 || calls > MaxBudgetCalls || b.sampleRemaining != 0 || calls > MaxBudgetCalls-b.processed {
		return errors.New("invalid result budget sample")
	}
	b.sampleUsed = 0
	b.sampleRemaining = calls
	return nil
}

// Allowance 为当前调用计算额度，后续空间按原始调用顺序预留。
func (b *ResultBudget) Allowance(capability CapabilityID) (int, error) {
	if b == nil || !capability.Valid() || b.sampleRemaining < 1 || b.processed >= MaxBudgetCalls {
		return 0, errors.New("invalid result budget call")
	}
	cap := MaxModelPreviewBytes
	if capability == CapabilityGlob || capability == CapabilityGrep {
		cap = MaxSearchPreviewBytes
	}
	sample := SamplePreviewBytes - b.sampleUsed - SafetyResultBytes*(b.sampleRemaining-1)
	turn := TurnPreviewBytes - b.turnUsed - SafetyResultBytes*(MaxBudgetCalls-b.processed-1)
	allowance := min(cap, sample, turn)
	if allowance < SafetyResultBytes {
		return 0, errors.New("result budget invariant violated")
	}
	return allowance, nil
}

// Commit 仅在最终预览形成后扣除实际 bytes，拒绝重复或超额提交。
func (b *ResultBudget) Commit(capability CapabilityID, metadata ResultBudgetMetadata) error {
	allowance, err := b.Allowance(capability)
	if err != nil {
		return err
	}
	if err := metadata.Validate(); err != nil {
		return err
	}
	if metadata.allowance != allowance {
		return errors.New("result budget allowance mismatch")
	}
	b.turnUsed += metadata.actual
	b.sampleUsed += metadata.actual
	b.processed++
	b.sampleRemaining--
	b.exhausted = b.exhausted || TurnPreviewBytes-b.turnUsed <= SafetyResultBytes*(MaxBudgetCalls-b.processed)
	return nil
}

// BodyExhausted 判断是否只剩每个后续调用的安全终态空间。
func (b *ResultBudget) BodyExhausted() bool {
	return b != nil && b.exhausted
}
func (b *ResultBudget) TurnUsed() int {
	if b == nil {
		return 0
	}
	return b.turnUsed
}
func (b *ResultBudget) SampleUsed() int {
	if b == nil {
		return 0
	}
	return b.sampleUsed
}

// Budget 返回最终预览的冻结计费事实。
func (r InvocationResult) Budget() ResultBudgetMetadata { return r.budget }

func initializeResultBudget(r InvocationResult) (InvocationResult, error) {
	cap := MaxModelPreviewBytes
	if r.capability == CapabilityGlob || r.capability == CapabilityGrep {
		cap = MaxSearchPreviewBytes
		r.searchSaving = ArtifactSaving{status: ArtifactNotNeeded}
	}
	r.budget = ResultBudgetMetadata{allowance: cap, actual: len(r.preview.text), reason: BudgetPreserved}
	return r, r.Validate()
}

// WithBudget 绑定 owner 分配的最终额度；不会改变正文或重建恢复结果。
func (r InvocationResult) WithBudget(metadata ResultBudgetMetadata) (InvocationResult, error) {
	r.budget = metadata
	return r, r.Validate()
}

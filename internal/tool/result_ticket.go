package tool

import "errors"

// ResultBudgetTicket 持有一个调用的额度；只有最终 close 才退休调用。
// 所有方法由 turn owner 顺序调用，不能跨 worker 并发共享。
type ResultBudgetTicket struct {
	owner            *ResultBudget
	capability       CapabilityID
	allowance        int
	reservation      int
	baseCommitted    bool
	baseActual       int
	feedbackActual   int
	feedbackSequence uint64
	closed           bool
}

// Reserve 固定额度并阻止后序调用准入，不扣除任何未提交 bytes。
func (b *ResultBudget) Reserve(capability CapabilityID, modelFeedback bool) (*ResultBudgetTicket, error) {
	allowance, err := b.Allowance(capability)
	if err != nil || b.active != nil {
		return nil, errors.New("result budget ticket already active or invalid")
	}
	reserve := 0
	if modelFeedback {
		reserve = min(MaxPostFeedbackBytes, max(0, allowance-SafetyResultBytes))
	}
	t := &ResultBudgetTicket{owner: b, capability: capability, allowance: allowance, reservation: reserve}
	b.active = t
	return t, nil
}
func (t *ResultBudgetTicket) Allowance() int     { return t.allowance }
func (t *ResultBudgetTicket) BaseAllowance() int { return t.allowance - t.reservation }
func (t *ResultBudgetTicket) Reservation() int   { return t.reservation }
func (t *ResultBudgetTicket) valid() bool {
	return t != nil && !t.closed && t.owner != nil && t.owner.active == t
}

// CommitBase 只在真实 result 已确定 Sync 后接纳该事实一次。
func (t *ResultBudgetTicket) CommitBase(metadata ResultBudgetMetadata) error {
	if !t.valid() || t.baseCommitted || metadata.Validate() != nil || metadata.Allowance() != t.BaseAllowance() {
		return errors.New("invalid result base commit")
	}
	t.baseActual = metadata.ActualBytes()
	t.baseCommitted = true
	t.owner.turnUsed += t.baseActual
	t.owner.sampleUsed += t.baseActual
	return nil
}

// CommitFeedback 接纳下一条已确定 Sync 的反馈增量，不推进调用计数。
func (t *ResultBudgetTicket) CommitFeedback(sequence uint64, delta int) error {
	if !t.valid() || !t.baseCommitted || sequence == 0 || sequence <= t.feedbackSequence || delta < 0 || delta > t.reservation-t.feedbackActual {
		return errors.New("invalid result feedback commit")
	}
	t.feedbackSequence = sequence
	t.feedbackActual += delta
	t.owner.turnUsed += delta
	t.owner.sampleUsed += delta
	return nil
}

// Close 在工具结果或后置 terminal 已 durable 后释放剩余额度，只退休一次。
func (t *ResultBudgetTicket) Close() error {
	if !t.valid() || !t.baseCommitted {
		return errors.New("invalid result ticket close")
	}
	b := t.owner
	t.closed = true
	b.active = nil
	b.processed++
	b.sampleRemaining--
	b.exhausted = b.exhausted || TurnPreviewBytes-b.turnUsed <= SafetyResultBytes*(MaxBudgetCalls-b.processed)
	return nil
}

// AbortReservation 仅释放尚无 durable 分段的预留，不退休调用。
func (t *ResultBudgetTicket) AbortReservation() error {
	if !t.valid() || t.baseCommitted || t.feedbackActual != 0 {
		return errors.New("result ticket reservation cannot be aborted")
	}
	t.closed = true
	t.owner.active = nil
	return nil
}

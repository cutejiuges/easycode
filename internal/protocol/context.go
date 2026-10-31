package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// ContextEstimatePayload 只公开不可逆的本地估算，不包含原生正文。
type ContextEstimatePayload struct {
	Method string                    `json:"method"`
	State  domain.TokenEstimateState `json:"state"`
	Tokens *uint64                   `json:"tokens,omitempty"`
}

// NewContextEstimatePayload 创建显式 known/unknown 估算投影。
func NewContextEstimatePayload(estimate domain.TokenEstimate) (ContextEstimatePayload, error) {
	if err := estimate.Validate(); err != nil {
		return ContextEstimatePayload{}, err
	}
	result := ContextEstimatePayload{Method: estimate.Method(), State: estimate.State()}
	if tokens, ok := estimate.Tokens(); ok {
		result.Tokens = &tokens
	}
	return result, nil
}

// Domain 严格恢复估算值。
func (value ContextEstimatePayload) Domain() (domain.TokenEstimate, error) {
	if value.State == domain.TokenEstimateEstimated && value.Tokens != nil {
		return domain.NewEstimatedTokenEstimate(value.Method, *value.Tokens)
	}
	if value.State == domain.TokenEstimateUnknown && value.Tokens == nil {
		return domain.NewUnknownTokenEstimate(value.Method)
	}
	return domain.TokenEstimate{}, fmt.Errorf("context estimate is invalid")
}

// ContextSourcePayload 表示固定分类的一项估算。
type ContextSourcePayload struct {
	Kind     string                 `json:"kind"`
	Revision string                 `json:"revision"`
	Estimate ContextEstimatePayload `json:"estimate"`
}

// ContextSnapshotPayload 保存已提交窗口的纯数值快照。
type ContextSnapshotPayload struct {
	Scope            string                 `json:"scope"`
	Revision         uint64                 `json:"revision"`
	Activity         string                 `json:"activity"`
	Model            string                 `json:"model"`
	Sources          []ContextSourcePayload `json:"sources"`
	Total            ContextEstimatePayload `json:"total"`
	WindowConfigured bool                   `json:"window_configured"`
	Window           uint64                 `json:"window"`
	ReservedOutput   uint64                 `json:"reserved_output"`
	SafetyMargin     uint64                 `json:"safety_margin"`
	BudgetState      string                 `json:"budget_state"`
}

// Validate 拒绝伪实时窗口、未知分类与不一致预算。
func (payload ContextSnapshotPayload) Validate() error {
	if payload.Scope != "committed" || payload.Model == "" {
		return fmt.Errorf("context snapshot scope or model is invalid")
	}
	switch payload.Activity {
	case "idle", "turn", "compacting":
	default:
		return fmt.Errorf("context activity is invalid")
	}
	if _, err := payload.Total.Domain(); err != nil {
		return err
	}
	expected := []string{"provider_profile", "tool_catalog", "committed_history", "current_input"}
	if len(payload.Sources) == 5 {
		expected = []string{"provider_profile", "tool_catalog", "project_instructions", "committed_history", "current_input"}
	}
	if len(payload.Sources) != len(expected) {
		return fmt.Errorf("context sources are invalid")
	}
	for i, source := range payload.Sources {
		if source.Kind != expected[i] || source.Revision == "" {
			return fmt.Errorf("context source is invalid")
		}
		if _, err := source.Estimate.Domain(); err != nil {
			return err
		}
	}
	if !payload.WindowConfigured {
		if payload.Window != 0 || payload.ReservedOutput != 0 || payload.SafetyMargin != 0 || payload.BudgetState != "not_enforced" {
			return fmt.Errorf("unconfigured context budget contains values")
		}
	} else {
		if payload.SafetyMargin >= payload.Window || payload.ReservedOutput >= payload.Window-payload.SafetyMargin {
			return fmt.Errorf("context budget reserves are invalid")
		}
		switch payload.BudgetState {
		case "within_limit", "over_limit", "indeterminate":
		default:
			return fmt.Errorf("context budget state is invalid")
		}
	}
	return nil
}

// CompactionStartedPayload 不包含临时 instructions 或摘要正文。
type CompactionStartedPayload struct {
	ContextRevision uint64 `json:"context_revision"`
}

// CompactionCompletedPayload 只公布窗口估算及独立摘要 usage。
type CompactionCompletedPayload struct {
	ContextRevision uint64                 `json:"context_revision"`
	Before          ContextEstimatePayload `json:"before"`
	After           ContextEstimatePayload `json:"after"`
	Usage           UsagePayload           `json:"usage"`
}

// CompactionFailureCode 是宿主及 Session 使用的封闭失败分类。
type CompactionFailureCode string

const (
	CompactionFailureCancelled     CompactionFailureCode = "compaction_cancelled"
	CompactionFailureTimeout       CompactionFailureCode = "compaction_timeout"
	CompactionFailureRequest       CompactionFailureCode = "compaction_request_failed"
	CompactionFailureResponse      CompactionFailureCode = "compaction_invalid_response"
	CompactionFailureBudget        CompactionFailureCode = "compaction_budget_exceeded"
	CompactionFailureUnknownBudget CompactionFailureCode = "compaction_budget_unknown"
	CompactionFailureNoReduction   CompactionFailureCode = "compaction_no_reduction"
	CompactionFailureInterrupted   CompactionFailureCode = "compaction_interrupted"
	CompactionFailureSession       CompactionFailureCode = "session_failed"
)

// Valid 校验封闭失败原因。
func (code CompactionFailureCode) Valid() bool {
	switch code {
	case CompactionFailureCancelled, CompactionFailureTimeout, CompactionFailureRequest, CompactionFailureResponse, CompactionFailureBudget, CompactionFailureUnknownBudget, CompactionFailureNoReduction, CompactionFailureInterrupted, CompactionFailureSession:
		return true
	default:
		return false
	}
}

// CompactionFailedPayload 显式区分已 durable 收口和无法确认的失败。
type CompactionFailedPayload struct {
	Code       CompactionFailureCode `json:"code"`
	Durable    bool                  `json:"durable"`
	UsageState string                `json:"usage_state"`
	Usage      *UsagePayload         `json:"usage,omitempty"`
}

// Validate 保持 unavailable 与 observed 的唯一字段组合。
func (payload CompactionFailedPayload) Validate() error {
	if !payload.Code.Valid() {
		return fmt.Errorf("compaction failure code is invalid")
	}
	switch payload.UsageState {
	case "unavailable":
		if payload.Usage != nil {
			return fmt.Errorf("unavailable compaction usage contains values")
		}
	case "observed":
		if payload.Usage == nil {
			return fmt.Errorf("observed compaction usage is missing")
		}
		if _, err := payload.Usage.Domain(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("compaction usage state is invalid")
	}
	return nil
}

// NewContextSnapshot 创建 thread 级查询事件。
func NewContextSnapshot(sessionID domain.SessionID, threadID domain.ThreadID, payload ContextSnapshotPayload) (Event, error) {
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	raw, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, err
	}
	event := newEvent(EventContextSnapshot)
	event.SessionID = sessionID
	event.ThreadID = threadID
	event.Payload = raw
	return event, event.Validate()
}

// DecodeContextSnapshot 严格解码查询快照。
func DecodeContextSnapshot(event Event) (ContextSnapshotPayload, error) {
	var payload ContextSnapshotPayload
	if event.Kind != EventContextSnapshot {
		return payload, fmt.Errorf("context event kind is invalid")
	}
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return payload, err
	}
	return payload, payload.Validate()
}

// NewCompactionStarted 创建独立操作的开始事件。
func NewCompactionStarted(sessionID domain.SessionID, threadID domain.ThreadID, id domain.CompactionID, revision uint64) (Event, error) {
	raw, err := codec.MarshalStable(CompactionStartedPayload{ContextRevision: revision})
	if err != nil {
		return Event{}, err
	}
	event := newEvent(EventCompactionStarted)
	event.SessionID = sessionID
	event.ThreadID = threadID
	event.CompactionID = id
	event.Payload = raw
	return event, event.Validate()
}

// DecodeCompactionStarted 严格解码开始事件。
func DecodeCompactionStarted(event Event) (CompactionStartedPayload, error) {
	var payload CompactionStartedPayload
	if event.Kind != EventCompactionStarted {
		return payload, fmt.Errorf("compaction event kind is invalid")
	}
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return payload, err
	}
	if payload.ContextRevision == 0 {
		return payload, fmt.Errorf("compaction context revision is invalid")
	}
	return payload, nil
}

// NewCompactionCompleted 创建 Sync 和 finalize 后的成功事件。
func NewCompactionCompleted(sessionID domain.SessionID, threadID domain.ThreadID, id domain.CompactionID, revision uint64, before, after domain.TokenEstimate, usage domain.SampleUsage) (Event, error) {
	beforePayload, err := NewContextEstimatePayload(before)
	if err != nil {
		return Event{}, err
	}
	afterPayload, err := NewContextEstimatePayload(after)
	if err != nil {
		return Event{}, err
	}
	usagePayload, err := NewUsagePayload(usage)
	if err != nil {
		return Event{}, err
	}
	raw, err := codec.MarshalStable(CompactionCompletedPayload{ContextRevision: revision, Before: beforePayload, After: afterPayload, Usage: usagePayload})
	if err != nil {
		return Event{}, err
	}
	event := newEvent(EventCompactionCompleted)
	event.SessionID = sessionID
	event.ThreadID = threadID
	event.CompactionID = id
	event.Payload = raw
	return event, event.Validate()
}

// DecodeCompactionCompleted 严格解码成功事件。
func DecodeCompactionCompleted(event Event) (CompactionCompletedPayload, error) {
	var payload CompactionCompletedPayload
	if event.Kind != EventCompactionCompleted {
		return payload, fmt.Errorf("compaction event kind is invalid")
	}
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return payload, err
	}
	return payload, payload.Validate()
}

// Validate 校验窗口缩减与独立 usage，不创建事件或时间戳。
func (payload CompactionCompletedPayload) Validate() error {
	if payload.ContextRevision < 2 {
		return fmt.Errorf("compaction context revision is invalid")
	}
	before, err := payload.Before.Domain()
	if err != nil {
		return err
	}
	after, err := payload.After.Domain()
	if err != nil {
		return err
	}
	old, oldKnown := before.Tokens()
	next, nextKnown := after.Tokens()
	if !oldKnown || !nextKnown || next >= old || before.Method() != after.Method() {
		return fmt.Errorf("compaction estimate did not decrease")
	}
	_, err = payload.Usage.Domain()
	return err
}

// NewCompactionFailed 创建唯一失败终态，正文和 raw usage 不进入事件。
func NewCompactionFailed(sessionID domain.SessionID, threadID domain.ThreadID, id domain.CompactionID, payload CompactionFailedPayload) (Event, error) {
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	raw, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, err
	}
	event := newEvent(EventCompactionFailed)
	event.SessionID = sessionID
	event.ThreadID = threadID
	event.CompactionID = id
	event.Payload = raw
	return event, event.Validate()
}

// DecodeCompactionFailed 严格解码失败终态。
func DecodeCompactionFailed(event Event) (CompactionFailedPayload, error) {
	var payload CompactionFailedPayload
	if event.Kind != EventCompactionFailed {
		return payload, fmt.Errorf("compaction event kind is invalid")
	}
	if err := codec.UnmarshalRecord(event.Payload, &payload); err != nil {
		return payload, err
	}
	return payload, payload.Validate()
}

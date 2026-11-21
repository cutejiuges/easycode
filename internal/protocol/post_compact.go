package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// PostCompactStatus 区分检查关闭结果与压缩成功事实。
type PostCompactStatus string

const (
	PostCompactSucceeded   PostCompactStatus = "succeeded"
	PostCompactFailed      PostCompactStatus = "failed"
	PostCompactSkipped     PostCompactStatus = "skipped"
	PostCompactCancelled   PostCompactStatus = "cancelled"
	PostCompactUncertain   PostCompactStatus = "outcome_uncertain"
	PostCompactInterrupted PostCompactStatus = "interrupted"
)

// ValidCode 校验运行或恢复关闭的封闭原因集合。
func (state PostCompactStatus) ValidCode(code domain.HookCode) bool {
	switch state {
	case PostCompactSucceeded:
		return code == domain.HookOK
	case PostCompactFailed:
		switch code {
		case domain.HookExitFailed, domain.HookStartFailed, domain.HookOutputLimit, domain.HookIOFailed, domain.HookTimedOut,
			domain.HookUnsupported, domain.HookSandboxUnavailable, domain.HookInputLimit, domain.HookExecutionBudgetExhausted:
			return true
		}
	case PostCompactSkipped:
		switch code {
		case domain.HookUnsupported, domain.HookSandboxUnavailable, domain.HookInputLimit, domain.HookTrustDenied:
			return true
		}
	case PostCompactCancelled:
		return code == domain.HookCancelled
	case PostCompactUncertain:
		return code == domain.HookOutcomeUncertain
	case PostCompactInterrupted:
		return code == domain.HookInterrupted
	}
	return false
}

// PostCompactInstalledPayload 标记已安装窗口，携带待检查计划或安全跳过原因。
type PostCompactInstalledPayload struct {
	Scope              domain.CompactionScope `json:"scope"`
	CheckpointSequence uint64                 `json:"checkpoint_sequence"`
	Revision           uint64                 `json:"revision"`
	PlanDigest         string                 `json:"plan_digest,omitempty"`
	Count              int                    `json:"count,omitempty"`
	SkipCode           domain.HookCode        `json:"skip_code,omitempty"`
}

// Validate 校验 checkpoint 身份以及计划和跳过原因的互斥关系。
func (payload PostCompactInstalledPayload) Validate() error {
	if payload.Scope.Validate() != nil || payload.CheckpointSequence == 0 || payload.Revision == 0 {
		return fmt.Errorf("invalid post compact installation")
	}
	switch payload.SkipCode {
	case "":
		if !domain.ValidHookDigest(payload.PlanDigest) || payload.Count < 1 || payload.Count > domain.MaxHookMatches {
			return fmt.Errorf("invalid post compact installed plan")
		}
	case domain.HookMatchLimit, domain.HookPlanLimit:
		if payload.PlanDigest != "" || payload.Count != 0 {
			return fmt.Errorf("skipped post compact installation contains a plan")
		}
	default:
		return fmt.Errorf("invalid post compact installation skip reason")
	}
	return nil
}

// NewPostCompactInstalled 创建仅供交互宿主使用的检查点安装通知。
func NewPostCompactInstalled(sessionID domain.SessionID, threadID domain.ThreadID, id domain.CompactionID, payload PostCompactInstalledPayload) (Event, error) {
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	raw, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, err
	}
	return newScopedPayloadEvent(EventPostCompactInstalled, sessionID, threadID, id, payload.Scope.TurnID, raw)
}

// DecodePostCompactInstalled 严格解码安装通知并核对压缩作用域。
func DecodePostCompactInstalled(event Event) (PostCompactInstalledPayload, error) {
	var payload PostCompactInstalledPayload
	if event.Kind != EventPostCompactInstalled || len(event.Payload) > 1<<10 || codec.UnmarshalRecord(event.Payload, &payload) != nil ||
		!event.CompactionID.Valid() || event.TurnID != payload.Scope.TurnID {
		return payload, fmt.Errorf("invalid post compact installation")
	}
	return payload, payload.Validate()
}

// PostCompactClosedPayload 投影当前运行的关闭结果。零关闭序号仅表示写入或投影不确定。
type PostCompactClosedPayload struct {
	Scope              domain.CompactionScope `json:"scope"`
	CheckpointSequence uint64                 `json:"checkpoint_sequence"`
	PlanDigest         string                 `json:"plan_digest"`
	CloseSequence      uint64                 `json:"close_sequence,omitempty"`
	State              PostCompactStatus      `json:"state"`
	Code               domain.HookCode        `json:"code"`
	Finished           int                    `json:"finished"`
}

// Validate 校验计划关联、关闭序号和安全结果。
func (payload PostCompactClosedPayload) Validate() error {
	if payload.Scope.Validate() != nil || payload.CheckpointSequence == 0 || !domain.ValidHookDigest(payload.PlanDigest) ||
		payload.Finished < 0 || payload.Finished > domain.MaxHookMatches || payload.State == PostCompactInterrupted ||
		!payload.State.ValidCode(payload.Code) {
		return fmt.Errorf("invalid post compact result")
	}
	if payload.CloseSequence == 0 && payload.State != PostCompactUncertain ||
		payload.CloseSequence != 0 && payload.CloseSequence <= payload.CheckpointSequence {
		return fmt.Errorf("invalid post compact closing sequence")
	}
	if payload.State == PostCompactSkipped && payload.Finished != 0 || payload.State == PostCompactSucceeded && payload.Finished == 0 {
		return fmt.Errorf("invalid post compact finished count")
	}
	return nil
}

// NewPostCompactClosed 创建独立于公共压缩终态的本地检查结果。
func NewPostCompactClosed(sessionID domain.SessionID, threadID domain.ThreadID, id domain.CompactionID, payload PostCompactClosedPayload) (Event, error) {
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	raw, err := codec.MarshalStable(payload)
	if err != nil {
		return Event{}, err
	}
	return newScopedPayloadEvent(EventPostCompactClosed, sessionID, threadID, id, payload.Scope.TurnID, raw)
}

// DecodePostCompactClosed 严格解码检查结果并核对压缩作用域。
func DecodePostCompactClosed(event Event) (PostCompactClosedPayload, error) {
	var payload PostCompactClosedPayload
	if event.Kind != EventPostCompactClosed || len(event.Payload) > 1<<10 || codec.UnmarshalRecord(event.Payload, &payload) != nil ||
		!event.CompactionID.Valid() || event.TurnID != payload.Scope.TurnID {
		return payload, fmt.Errorf("invalid post compact result")
	}
	return payload, payload.Validate()
}

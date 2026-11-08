package protocol

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

type HookTrustDecision string

const (
	HookTrustSession HookTrustDecision = "trust_session"
	HookTrustDeny    HookTrustDecision = "deny"
)

func (d HookTrustDecision) Valid() bool { return d == HookTrustSession || d == HookTrustDeny }

// HookTrustRequestedPayload 是完整但仅限本地瞬态展示的审查声明。
type HookTrustRequestedPayload struct {
	RequestID    string                 `json:"request_id"`
	InvocationID string                 `json:"invocation_id"`
	Capability   domain.HookCapability  `json:"capability"`
	Declaration  domain.HookDeclaration `json:"declaration"`
}

func (p HookTrustRequestedPayload) Validate() error {
	if !validUUIDv7Text(p.RequestID) || !validUUIDv7Text(p.InvocationID) || !p.Capability.Valid() || p.Declaration.Validate() != nil || !p.Declaration.Matcher.Matches(p.Capability) {
		return fmt.Errorf("invalid hook trust request")
	}
	return nil
}

// HookTrustResponse 绑定当前真实调用和快照，不能授权其他来源或会话。
type HookTrustResponse struct {
	SessionID    domain.SessionID     `json:"session_id"`
	ThreadID     domain.ThreadID      `json:"thread_id"`
	TurnID       domain.TurnID        `json:"turn_id"`
	RequestID    string               `json:"request_id"`
	InvocationID string               `json:"invocation_id"`
	Reference    domain.HookReference `json:"reference"`
	Decision     HookTrustDecision    `json:"decision"`
}

func NewHookTrustResponse(session domain.SessionID, thread domain.ThreadID, turn domain.TurnID, p HookTrustRequestedPayload, decision HookTrustDecision) (HookTrustResponse, error) {
	if p.Validate() != nil {
		return HookTrustResponse{}, fmt.Errorf("invalid hook trust request")
	}
	r := HookTrustResponse{session, thread, turn, p.RequestID, p.InvocationID, p.Declaration.Reference, decision}
	return r, r.Validate()
}
func (r HookTrustResponse) Validate() error {
	if !r.SessionID.Valid() || !r.ThreadID.Valid() || !r.TurnID.Valid() || !validUUIDv7Text(r.RequestID) || !validUUIDv7Text(r.InvocationID) || r.Reference.Validate() != nil || !r.Decision.Valid() {
		return fmt.Errorf("invalid hook trust response")
	}
	return nil
}

// HookTrustTarget 只用于空闲时清除当前实例授权，不创建执行身份。
type HookTrustTarget struct {
	SessionID domain.SessionID     `json:"session_id"`
	ThreadID  domain.ThreadID      `json:"thread_id"`
	Reference domain.HookReference `json:"reference"`
}

func NewHookTrustTarget(session domain.SessionID, thread domain.ThreadID, ref domain.HookReference) (HookTrustTarget, error) {
	v := HookTrustTarget{session, thread, ref}
	return v, v.Validate()
}
func (t HookTrustTarget) Validate() error {
	if !t.SessionID.Valid() || !t.ThreadID.Valid() || t.Reference.Validate() != nil {
		return fmt.Errorf("invalid hook trust target")
	}
	return nil
}

func NewResolveHookTrustCommand(id RequestID, r HookTrustResponse) (Command, error) {
	c := Command{kind: CommandResolveHookTrust, requestID: id, hookTrust: r}
	return c, c.Validate()
}
func NewResetHookTrustCommand(id RequestID, t HookTrustTarget) (Command, error) {
	c := Command{kind: CommandResetHookTrust, requestID: id, hookTarget: t}
	return c, c.Validate()
}
func DecodeResolveHookTrustCommand(id RequestID, b []byte) (Command, error) {
	var r HookTrustResponse
	if len(b) > 16<<10 || codec.UnmarshalRecord(b, &r) != nil {
		return Command{}, fmt.Errorf("invalid hook trust command")
	}
	return NewResolveHookTrustCommand(id, r)
}
func DecodeResetHookTrustCommand(id RequestID, b []byte) (Command, error) {
	var t HookTrustTarget
	if len(b) > 16<<10 || codec.UnmarshalRecord(b, &t) != nil {
		return Command{}, fmt.Errorf("invalid hook reset command")
	}
	return NewResetHookTrustCommand(id, t)
}
func (c Command) HookTrustResponse() HookTrustResponse { return c.hookTrust }
func (c Command) HookTrustTarget() HookTrustTarget     { return c.hookTarget }

func NewHookTrustRequested(p HookTrustRequestedPayload) (Event, error) {
	if p.Validate() != nil {
		return Event{}, fmt.Errorf("invalid hook trust request")
	}
	return newEventWithPayload(EventHookTrustRequested, p)
}
func DecodeHookTrustRequested(e Event) (HookTrustRequestedPayload, error) {
	var p HookTrustRequestedPayload
	if e.Kind != EventHookTrustRequested || len(e.Payload) > 16<<10 || codec.UnmarshalRecord(e.Payload, &p) != nil {
		return p, fmt.Errorf("invalid hook trust request")
	}
	return p, p.Validate()
}
func NewHookTrustResolved(p HookTrustResponse) (Event, error) {
	if p.Validate() != nil {
		return Event{}, fmt.Errorf("invalid hook trust resolution")
	}
	return newEventWithPayload(EventHookTrustResolved, p)
}
func DecodeHookTrustResolved(e Event) (HookTrustResponse, error) {
	var p HookTrustResponse
	if e.Kind != EventHookTrustResolved || len(e.Payload) > 16<<10 || codec.UnmarshalRecord(e.Payload, &p) != nil {
		return p, fmt.Errorf("invalid hook trust resolution")
	}
	return p, p.Validate()
}

// HookActivityState 将瞬态检查投影与持久化执行 outcome 分开。
type HookActivityState string

const (
	HookActivityChecking  HookActivityState = "checking"
	HookActivityCompleted HookActivityState = "completed"
	HookActivitySkipped   HookActivityState = "skipped"
)

// HookActivityPayload 是由 durable 检查边界产生的安全投影，不能冒充工具 started。
type HookActivityPayload struct {
	InvocationID string               `json:"invocation_id"`
	Reference    domain.HookReference `json:"reference"`
	State        HookActivityState    `json:"state"`
	Code         domain.HookCode      `json:"code"`
	StdoutBytes  int64                `json:"stdout_bytes"`
	StderrBytes  int64                `json:"stderr_bytes"`
}

func (p HookActivityPayload) Validate() error {
	if !validUUIDv7Text(p.InvocationID) || p.Reference.Validate() != nil || p.StdoutBytes < 0 || p.StderrBytes < 0 {
		return fmt.Errorf("invalid hook activity")
	}
	if p.State == "checking" {
		if p.Code != domain.HookOK || p.StdoutBytes != 0 || p.StderrBytes != 0 {
			return fmt.Errorf("invalid hook start activity")
		}
		return nil
	}
	if p.Reference.Event == domain.HookPostToolUse {
		switch p.State {
		case HookActivityCompleted:
			if p.Code == domain.HookOK || domain.HookError.ValidCode(p.Code) {
				return nil
			}
			return fmt.Errorf("invalid post hook completion activity")
		case HookActivitySkipped:
			switch p.Code {
			case domain.HookToolNotSuccessful, domain.HookTrustDenied, domain.HookTrustRequired, domain.HookInputLimit, domain.HookExecutionBudgetExhausted:
				return nil
			}
			return fmt.Errorf("invalid post hook skipped activity")
		}
		if p.State == HookActivityState(domain.HookCancel) && p.Code == domain.HookPostInterrupted {
			return nil
		}
	}
	if !domain.HookOutcome(p.State).ValidCode(p.Code) {
		return fmt.Errorf("invalid hook activity outcome")
	}
	return nil
}
func NewHookActivity(p HookActivityPayload) (Event, error) {
	if p.Validate() != nil {
		return Event{}, fmt.Errorf("invalid hook activity")
	}
	return newEventWithPayload(EventHookActivity, p)
}
func DecodeHookActivity(e Event) (HookActivityPayload, error) {
	var p HookActivityPayload
	if e.Kind != EventHookActivity || len(e.Payload) > 16<<10 || codec.UnmarshalRecord(e.Payload, &p) != nil {
		return p, fmt.Errorf("invalid hook activity")
	}
	return p, p.Validate()
}

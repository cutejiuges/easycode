package session

import (
	"encoding/json"
	"fmt"
	"slices"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// SessionStartPlan 冻结一次真实激活或压缩的有序检查。
type SessionStartPlan struct {
	target domain.SessionStartTarget
	checks []sessionStartCheck
	digest string
}

type sessionStartCheck struct {
	Reference domain.HookReference `json:"reference"`
	Matcher   string               `json:"matcher"`
}

func (v *sessionStartCheck) UnmarshalJSON(data []byte) error {
	type fields sessionStartCheck
	var decoded fields
	if err := codec.DecodeObject(data, &decoded, "reference", "matcher"); err != nil {
		return err
	}
	*v = sessionStartCheck(decoded)
	return nil
}

type sessionStartPlanFields struct {
	Target domain.SessionStartTarget `json:"target"`
	Checks []sessionStartCheck       `json:"checks"`
}

type sessionStartPlanWire struct {
	sessionStartPlanFields
	Digest string `json:"digest"`
}

// NewSessionStartPlan 在持久化前校验声明及其实际匹配范围。
func NewSessionStartPlan(target domain.SessionStartTarget, declarations []domain.HookDeclaration) (SessionStartPlan, error) {
	checks := make([]sessionStartCheck, len(declarations))
	for index, declaration := range declarations {
		matcher, ok := declaration.Scope.SessionStartMatcher()
		if !ok || declaration.Validate() != nil {
			return SessionStartPlan{}, fmt.Errorf("invalid session start declaration")
		}
		checks[index] = sessionStartCheck{Reference: declaration.Reference, Matcher: matcher}
	}
	return buildSessionStartPlan(sessionStartPlanFields{Target: target, Checks: checks})
}

func buildSessionStartPlan(fields sessionStartPlanFields) (SessionStartPlan, error) {
	if fields.Target.Validate() != nil || len(fields.Checks) == 0 || len(fields.Checks) > domain.MaxHookMatches {
		return SessionStartPlan{}, fmt.Errorf("invalid session start plan")
	}
	plan := SessionStartPlan{target: fields.Target, checks: fields.Checks}
	if err := domain.ValidateSessionStartReferences(plan.References()); err != nil {
		return SessionStartPlan{}, fmt.Errorf("invalid session start declarations")
	}
	for _, check := range fields.Checks {
		if check.Matcher != "*" && check.Matcher != string(fields.Target.Source) {
			return SessionStartPlan{}, fmt.Errorf("invalid session start check")
		}
	}
	encoded, err := codec.MarshalStable(fields)
	if err != nil {
		return SessionStartPlan{}, err
	}
	plan.digest = domain.HookDigest(encoded)
	return plan, nil
}

func (p SessionStartPlan) Target() domain.SessionStartTarget { return p.target }
func (p SessionStartPlan) Digest() string                    { return p.digest }
func (p SessionStartPlan) References() []domain.HookReference {
	references := make([]domain.HookReference, len(p.checks))
	for index, check := range p.checks {
		references[index] = check.Reference
	}
	return references
}

// NewSessionStartHookPlannedDraft 保存待审批或运行的实际关联和检查顺序。
func NewSessionStartHookPlannedDraft(plan SessionStartPlan) (RecordDraft, error) {
	if plan.digest == "" {
		return RecordDraft{}, fmt.Errorf("invalid session start plan")
	}
	wire := sessionStartPlanWire{sessionStartPlanFields{plan.target, plan.checks}, plan.digest}
	return encodeDraft(EventSessionStartHookPlanned, "", plan.target.TurnID, wire)
}

// DecodeSessionStartHookPlanned 重算计划身份并核对信封关联。
func DecodeSessionStartHookPlanned(record Record) (SessionStartPlan, error) {
	var wire sessionStartPlanWire
	if record.EventKind != EventSessionStartHookPlanned || codec.DecodeObject(record.Payload, &wire, "target", "checks", "digest") != nil {
		return SessionStartPlan{}, fmt.Errorf("invalid session start plan encoding")
	}
	plan, err := buildSessionStartPlan(wire.sessionStartPlanFields)
	if err != nil || plan.digest != wire.Digest || !matchesSessionStartEnvelope(record, plan.target) {
		return SessionStartPlan{}, fmt.Errorf("invalid session start plan evidence")
	}
	return plan, nil
}

// NewSessionStartHookStartedDraft 保存运行前的真实序位与准入身份。
func NewSessionStartHookStartedDraft(identity domain.HookRunIdentity) (RecordDraft, error) {
	target, ok := sessionStartRunTarget(identity)
	if !ok {
		return RecordDraft{}, fmt.Errorf("invalid session start admission")
	}
	return encodeDraft(EventSessionStartHookStarted, "", target.TurnID, identity)
}

func sessionStartRunTarget(identity domain.HookRunIdentity) (domain.SessionStartTarget, bool) {
	target, ok := identity.Target.SessionStart()
	return target, ok && identity.Event == domain.HookSessionStart && identity.Validate() == nil
}

// DecodeSessionStartHookStarted 读取单次实际执行准入。
func DecodeSessionStartHookStarted(record Record) (domain.HookRunIdentity, error) {
	var identity domain.HookRunIdentity
	if record.EventKind != EventSessionStartHookStarted || decodeSessionStartRunIdentity(record.Payload, &identity) != nil {
		return domain.HookRunIdentity{}, fmt.Errorf("invalid session start admission encoding")
	}
	target, ok := sessionStartRunTarget(identity)
	if !ok || !matchesSessionStartEnvelope(record, target) {
		return domain.HookRunIdentity{}, fmt.Errorf("invalid session start admission")
	}
	return identity, nil
}

func decodeSessionStartRunIdentity(payload []byte, identity *domain.HookRunIdentity) error {
	return codec.DecodeObject(payload, identity, "event", "target", "plan_digest", "ordinal", "run_id")
}

// SessionStartFinishedPayload 仅保存统计和可能获准交付的单项摘要。
type SessionStartFinishedPayload struct {
	Result domain.HookRunResult      `json:"result"`
	Proof  *domain.SessionStartProof `json:"proof,omitempty"`
}

func (payload SessionStartFinishedPayload) validate() (domain.SessionStartTarget, error) {
	result := payload.Result
	target, ok := sessionStartRunTarget(result.Identity)
	if !ok || result.Validate() != nil || result.Origin != "execution" || result.Status == domain.HookBlock {
		return domain.SessionStartTarget{}, fmt.Errorf("invalid session start result")
	}
	if p := payload.Proof; p != nil {
		if p.Validate() != nil || p.Ordinal != result.Identity.Ordinal || result.Status != domain.HookContinue {
			return domain.SessionStartTarget{}, fmt.Errorf("invalid session start candidate evidence")
		}
	}
	return target, nil
}

// NewSessionStartHookFinishedDraft 从真实 runner 结论持久化受限执行事实。
func NewSessionStartHookFinishedDraft(result domain.HookRunResult, proof *domain.SessionStartProof) (RecordDraft, error) {
	payload := SessionStartFinishedPayload{Result: result, Proof: proof}
	target, err := payload.validate()
	if err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventSessionStartHookFinished, "", target.TurnID, payload)
}

// DecodeSessionStartHookFinished 校验运行统计及无正文的模型候选证据。
func DecodeSessionStartHookFinished(record Record) (SessionStartFinishedPayload, error) {
	var wire struct {
		Result json.RawMessage           `json:"result"`
		Proof  *domain.SessionStartProof `json:"proof,omitempty"`
	}
	var payload SessionStartFinishedPayload
	if record.EventKind != EventSessionStartHookFinished || codec.DecodeObject(record.Payload, &wire, "result") != nil {
		return payload, fmt.Errorf("invalid session start result encoding")
	}
	resultFields, err := codec.ObjectMembers(wire.Result)
	if err != nil || codec.DecodeObject(wire.Result, &payload.Result, "identity", "status", "code", "exit_known",
		"exit_code", "timed_out", "cancelled", "stdout_bytes", "stderr_bytes", "cleanup_confirmed", "origin") != nil ||
		decodeSessionStartRunIdentity(resultFields["identity"], &payload.Result.Identity) != nil {
		return payload, fmt.Errorf("invalid session start run result encoding")
	}
	payload.Proof = wire.Proof
	target, err := payload.validate()
	if err != nil || !matchesSessionStartEnvelope(record, target) {
		return payload, fmt.Errorf("invalid session start result evidence")
	}
	return payload, nil
}

// SessionStartCloseState 区分实际交付、未交付和中断状态。
type SessionStartCloseState string

const (
	SessionStartApplied     SessionStartCloseState = "applied"
	SessionStartNoContext   SessionStartCloseState = "no_context"
	SessionStartSkipped     SessionStartCloseState = "skipped"
	SessionStartCancelled   SessionStartCloseState = "cancelled"
	SessionStartUncertain   SessionStartCloseState = "outcome_uncertain"
	SessionStartInterrupted SessionStartCloseState = "interrupted"
)

// SessionStartRestoreEvidence 是关闭事实对实际 Provider 上下文的预期证明。
type SessionStartRestoreEvidence struct {
	Target domain.SessionStartTarget
	Proofs []domain.SessionStartProof
}

// SessionStartClosedPayload 以一个封闭分支表示计划的本地结算和原生交付。
type SessionStartClosedPayload struct {
	Target     domain.SessionStartTarget  `json:"target"`
	PlanDigest string                     `json:"plan_digest"`
	Finished   int                        `json:"finished"`
	State      SessionStartCloseState     `json:"state"`
	Code       domain.HookCode            `json:"code"`
	Origin     string                     `json:"origin"`
	Proofs     []domain.SessionStartProof `json:"proofs,omitempty"`
	Native     *NativeCommitPayload       `json:"native,omitempty"`
}

func (p SessionStartClosedPayload) validate() error {
	if p.Target.Validate() != nil || !domain.ValidHookDigest(p.PlanDigest) ||
		p.Finished < 0 || p.Finished > domain.MaxHookMatches || (p.Origin != "execution" && p.Origin != "recovery") {
		return fmt.Errorf("invalid session start closure")
	}
	if p.State != SessionStartApplied && (p.Native != nil || p.Proofs != nil) {
		return fmt.Errorf("invalid undelivered session start context")
	}
	if p.Origin == "recovery" && p.State != SessionStartInterrupted && p.State != SessionStartUncertain {
		return fmt.Errorf("invalid session start recovery closure")
	}
	switch p.State {
	case SessionStartApplied:
		if (p.Code == domain.HookOK || p.Code == domain.HookExecutionBudgetExhausted) &&
			p.Native != nil && validateNativeCommitPayload(*p.Native) == nil &&
			len(p.Proofs) > 0 && len(p.Proofs) <= p.Finished && validSessionStartProofList(p.Proofs) {
			return nil
		}
	case SessionStartNoContext:
		if p.Code == domain.HookOK {
			return nil
		}
	case SessionStartSkipped:
		if validSessionStartSkipCode(p.Code) {
			return nil
		}
	case SessionStartCancelled:
		if p.Code == domain.HookCancelled {
			return nil
		}
	case SessionStartUncertain:
		if p.Code == domain.HookOutcomeUncertain {
			return nil
		}
	case SessionStartInterrupted:
		if p.Origin == "recovery" && p.Code == domain.HookInterrupted {
			return nil
		}
	}
	return fmt.Errorf("invalid session start closure state")
}

func validSessionStartProofList(proofs []domain.SessionStartProof) bool {
	previous, size := -1, 0
	for _, proof := range proofs {
		if proof.Validate() != nil || proof.Ordinal <= previous {
			return false
		}
		previous = proof.Ordinal
		size += proof.Bytes
	}
	return size <= domain.MaxSessionStartContextBytes
}

func validSessionStartSkipCode(code domain.HookCode) bool {
	switch code {
	case domain.HookTrustDenied, domain.HookUnsupported, domain.HookSandboxUnavailable, domain.HookInputLimit,
		domain.HookExecutionBudgetExhausted, domain.HookFeedbackBudgetExceeded, domain.HookPlanLimit:
		return true
	}
	return domain.HookError.ValidCode(code)
}

// NewSessionStartHookClosedDraft 构造独立的关闭记录或原子首轮批次的首项。
func NewSessionStartHookClosedDraft(plan SessionStartPlan, finished int, state SessionStartCloseState, code domain.HookCode, proofs []domain.SessionStartProof, envelope provider.NativeCommitEnvelope) (RecordDraft, error) {
	payload := SessionStartClosedPayload{Target: plan.target, PlanDigest: plan.digest, Finished: finished, State: state, Code: code, Origin: "execution", Proofs: slices.Clone(proofs)}
	if envelope.Family() != "" {
		payload.Native = &NativeCommitPayload{Provider: envelope.Family(), Wire: envelope.Wire(), Payload: envelope.Payload()}
	}
	if finished > len(plan.checks) {
		return RecordDraft{}, fmt.Errorf("session start closure exceeds plan")
	}
	return sessionStartCloseDraft(payload)
}

func sessionStartCloseDraft(payload SessionStartClosedPayload) (RecordDraft, error) {
	if err := payload.validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventSessionStartHookClosed, "", payload.Target.TurnID, payload)
}

// DecodeSessionStartHookClosed 校验当前关闭分支并返回应交付的唯一原生信封。
func DecodeSessionStartHookClosed(record Record) (SessionStartClosedPayload, error) {
	var payload SessionStartClosedPayload
	if record.EventKind != EventSessionStartHookClosed || codec.DecodeObject(record.Payload, &payload, "target", "plan_digest", "finished", "state", "code", "origin") != nil {
		return payload, fmt.Errorf("invalid session start closure encoding")
	}
	if err := payload.validate(); err != nil || !matchesSessionStartEnvelope(record, payload.Target) {
		return payload, fmt.Errorf("invalid session start closure evidence")
	}
	return payload, nil
}

func matchesSessionStartEnvelope(record Record, target domain.SessionStartTarget) bool {
	return record.ParentThreadID == "" && record.TurnID == target.TurnID
}

func validateSessionStartRecordShape(record Record) error {
	size := uint32(1)
	if record.EventKind == EventSessionStartHookClosed {
		payload, err := DecodeSessionStartHookClosed(record)
		if err != nil {
			return err
		}
		if payload.State == SessionStartApplied && payload.Target.Source != domain.SessionStartCompact {
			size = 3
		}
	} else if err := decodeKnownRecord(record); err != nil {
		return err
	}
	if record.BatchID != record.Sequence || record.BatchSize != size || record.BatchIndex != 0 {
		return fmt.Errorf("invalid session start record batch shape")
	}
	return nil
}

// validateSessionStartBatchPrefix 校验尾批中每条已经完整读取的真实输入事实。
func validateSessionStartBatchPrefix(first, next Record) error {
	if first.EventKind != EventSessionStartHookClosed {
		return nil
	}
	if next.ParentThreadID == "" && next.TurnID == first.TurnID {
		switch next.BatchIndex {
		case 1:
			input, err := DecodeTurnInputsPayload(next)
			if err == nil && input.Placement == domain.InputInitial && input.TargetSampleIndex == 0 {
				return nil
			}
		case 2:
			if next.EventKind == EventProviderNativeCommit {
				return nil
			}
		}
	}
	return fmt.Errorf("invalid session start input prefix")
}

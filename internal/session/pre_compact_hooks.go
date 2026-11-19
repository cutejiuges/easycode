package session

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

type preCompactPlanKind string

const (
	preCompactNone    preCompactPlanKind = "none"
	preCompactInvalid preCompactPlanKind = "invalid"
	preCompactPlanned preCompactPlanKind = "planned"
)

// PreCompactPlan 冻结压缩前门禁所需证据，禁止保存命令与重点原文。
type PreCompactPlan struct {
	kind     preCompactPlanKind
	cause    domain.HookCode
	checks   []preCompactReference
	original PreCompactFocusEvidence
	digest   string
}

type preCompactReference struct {
	Reference domain.HookReference `json:"reference"`
	Matcher   string               `json:"matcher"`
}

// PreCompactFocusEvidence 仅保存规范化重点的字节数与摘要。
type PreCompactFocusEvidence struct {
	Bytes  int    `json:"utf8_bytes"`
	Digest string `json:"digest"`
}

type preCompactNoneWire struct {
	Kind preCompactPlanKind `json:"kind"`
}

type preCompactInvalidWire struct {
	Kind  preCompactPlanKind `json:"kind"`
	Cause domain.HookCode    `json:"cause"`
}

type preCompactPlannedWire struct {
	Kind     preCompactPlanKind      `json:"kind"`
	Checks   []preCompactReference   `json:"checks"`
	Original PreCompactFocusEvidence `json:"original_focus"`
	Digest   string                  `json:"plan_digest"`
}

// NewNoPreCompactPlan 表示来源已成功读取且本次压缩没有匹配检查。
func NewNoPreCompactPlan() PreCompactPlan {
	return PreCompactPlan{kind: preCompactNone}
}

// NewInvalidPreCompactPlan 保留来源配置或匹配选择的失败原因。
func NewInvalidPreCompactPlan(cause domain.HookCode) (PreCompactPlan, error) {
	if cause != domain.HookConfigInvalid && cause != domain.HookMatchLimit {
		return PreCompactPlan{}, fmt.Errorf("invalid pre-compact plan cause")
	}
	return PreCompactPlan{kind: preCompactInvalid, cause: cause}, nil
}

// NewPreCompactPlan 从冻结声明构造仅包含引用和摘要证据的计划。
func NewPreCompactPlan(id domain.CompactionID, scope domain.CompactionScope, source provider.CompactionSource, focus string, declarations []domain.HookDeclaration) (PreCompactPlan, error) {
	if len(declarations) == 0 || len(declarations) > domain.MaxHookMatches {
		return PreCompactPlan{}, fmt.Errorf("invalid pre-compact declaration count")
	}
	original, err := newPreCompactFocusEvidence(focus, domain.MaxCompactionInstructionsBytes)
	if err != nil {
		return PreCompactPlan{}, err
	}
	plan := PreCompactPlan{kind: preCompactPlanned, original: original, checks: make([]preCompactReference, len(declarations))}
	for i, declaration := range declarations {
		matcher, ok := declaration.Scope.CompactionMatcher()
		if !ok || declaration.Validate() != nil {
			return PreCompactPlan{}, fmt.Errorf("invalid pre-compact declaration")
		}
		plan.checks[i] = preCompactReference{Reference: declaration.Reference, Matcher: matcher}
	}
	if err := plan.validateChecks(scope); err != nil {
		return PreCompactPlan{}, err
	}
	plan.digest, err = plan.computeDigest(id, scope, source)
	if err != nil {
		return PreCompactPlan{}, err
	}
	return plan, nil
}

// Digest 返回绑定操作与有序声明的计划摘要。
func (plan PreCompactPlan) Digest() string { return plan.digest }

func (plan PreCompactPlan) computeDigest(id domain.CompactionID, scope domain.CompactionScope, source provider.CompactionSource) (string, error) {
	if !id.Valid() || scope.Validate() != nil || source.Validate() != nil {
		return "", fmt.Errorf("invalid pre-compact operation")
	}
	data, err := codec.MarshalStable(struct {
		ID       domain.CompactionID       `json:"compaction_id"`
		Scope    domain.CompactionScope    `json:"scope"`
		Source   provider.CompactionSource `json:"source"`
		Original PreCompactFocusEvidence   `json:"original_focus"`
		Checks   []preCompactReference     `json:"checks"`
	}{id, scope, source, plan.original, plan.checks})
	if err != nil {
		return "", fmt.Errorf("encode pre-compact plan digest: %w", err)
	}
	return domain.HookDigest(data), nil
}

func (plan PreCompactPlan) validate(id domain.CompactionID, scope domain.CompactionScope, source provider.CompactionSource) error {
	switch plan.kind {
	case preCompactNone, preCompactInvalid:
		if !id.Valid() || scope.Validate() != nil {
			return fmt.Errorf("invalid pre-compact operation")
		}
		return source.Validate()
	case preCompactPlanned:
		if err := plan.validateChecks(scope); err != nil {
			return err
		}
		digest, err := plan.computeDigest(id, scope, source)
		if err != nil || plan.digest != digest {
			return fmt.Errorf("invalid pre-compact plan digest")
		}
		return nil
	default:
		return fmt.Errorf("invalid pre-compact plan kind")
	}
}

func (plan PreCompactPlan) validateChecks(scope domain.CompactionScope) error {
	if len(plan.checks) == 0 || len(plan.checks) > domain.MaxHookMatches || !plan.original.valid(0, domain.MaxCompactionInstructionsBytes) || (scope.Trigger == domain.CompactionAuto && plan.original.Bytes != 0) {
		return fmt.Errorf("invalid pre-compact plan contents")
	}
	seen := make(map[preCompactDeclarationID]struct{}, len(plan.checks))
	revisions := make(map[domain.HookSource]string, 2)
	for _, check := range plan.checks {
		ref := check.Reference
		declarationID := preCompactDeclarationID{ref.Source, ref.ID}
		_, duplicate := seen[declarationID]
		matcher, err := domain.ParsePreCompactHookScope(check.Matcher)
		if err != nil || !matcher.MatchesCompaction(scope.Trigger) || ref.Event != domain.HookPreCompact || ref.Validate() != nil || duplicate {
			return fmt.Errorf("invalid pre-compact plan declaration")
		}
		if (ref.Source == domain.HookSourceUser && revisions[domain.HookSourceProject] != "") || (revisions[ref.Source] != "" && revisions[ref.Source] != ref.Revision) {
			return fmt.Errorf("invalid pre-compact declaration order or revision")
		}
		seen[declarationID] = struct{}{}
		revisions[ref.Source] = ref.Revision
	}
	return nil
}

type preCompactDeclarationID struct {
	source domain.HookSource
	id     string
}

// MarshalJSON 编码唯一匹配分支，避免为空分支写入无关证据。
func (plan PreCompactPlan) MarshalJSON() ([]byte, error) {
	switch plan.kind {
	case preCompactNone:
		return codec.MarshalStable(preCompactNoneWire{Kind: plan.kind})
	case preCompactInvalid:
		return codec.MarshalStable(preCompactInvalidWire{Kind: plan.kind, Cause: plan.cause})
	case preCompactPlanned:
		return codec.MarshalStable(preCompactPlannedWire{Kind: plan.kind, Checks: plan.checks, Original: plan.original, Digest: plan.digest})
	default:
		return nil, fmt.Errorf("invalid pre-compact plan kind")
	}
}

// UnmarshalJSON 按分支严格解码。操作绑定由开始事实验证。
func (plan *PreCompactPlan) UnmarshalJSON(data []byte) error {
	fields, err := codec.ObjectMembers(data)
	if err != nil {
		return err
	}
	var kind preCompactPlanKind
	if err := codec.Unmarshal(fields["kind"], &kind); err != nil {
		return fmt.Errorf("invalid pre-compact plan kind")
	}
	switch kind {
	case preCompactNone:
		var wire preCompactNoneWire
		if err := codec.UnmarshalRecord(data, &wire); err != nil {
			return err
		}
		*plan = NewNoPreCompactPlan()
	case preCompactInvalid:
		var wire preCompactInvalidWire
		if err := codec.UnmarshalRecord(data, &wire); err != nil {
			return err
		}
		value, err := NewInvalidPreCompactPlan(wire.Cause)
		if err != nil {
			return err
		}
		*plan = value
	case preCompactPlanned:
		var wire preCompactPlannedWire
		if err := codec.UnmarshalRecord(data, &wire); err != nil {
			return err
		}
		*plan = PreCompactPlan{kind: kind, checks: wire.Checks, original: wire.Original, digest: wire.Digest}
	default:
		return fmt.Errorf("invalid pre-compact plan kind")
	}
	return nil
}

func newPreCompactFocusEvidence(focus string, maximum int) (PreCompactFocusEvidence, error) {
	if !utf8.ValidString(focus) || len(focus) > maximum || focus != strings.TrimSpace(focus) {
		return PreCompactFocusEvidence{}, fmt.Errorf("invalid pre-compact focus")
	}
	return PreCompactFocusEvidence{Bytes: len(focus), Digest: domain.HookDigest([]byte(focus))}, nil
}

func (evidence PreCompactFocusEvidence) valid(minimum, maximum int) bool {
	if evidence.Bytes < minimum || evidence.Bytes > maximum || !domain.ValidHookDigest(evidence.Digest) {
		return false
	}
	return (evidence.Bytes == 0) == (evidence.Digest == domain.HookDigest(nil))
}

// PreCompactHookFinishedPayload 保存实际执行结果与可选的安全贡献证据。
type PreCompactHookFinishedPayload struct {
	Result domain.HookRunResult     `json:"result"`
	Focus  *PreCompactFocusEvidence `json:"focus,omitempty"`
}

// PreCompactHookDecidedPayload 冻结检查前缀对应的网络准入结论。
type PreCompactHookDecidedPayload struct {
	Target     domain.HookTarget              `json:"target"`
	PlanDigest string                         `json:"plan_digest"`
	Finished   int                            `json:"finished"`
	Code       protocol.CompactionFailureCode `json:"failure_code"`
	Cause      domain.HookCode                `json:"cause"`
}

// NewPreCompactHookStartedDraft 记录交付执行器前的真实准入意图。
func NewPreCompactHookStartedDraft(id domain.HookRunIdentity) (RecordDraft, error) {
	if id.Event != domain.HookPreCompact || id.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid pre-compact execution identity")
	}
	return encodeDraft(EventPreCompactHookStarted, "", "", id)
}

// NewPreCompactHookFinishedDraft 编码真实结果，检查重点原文不会离开构造边界。
func NewPreCompactHookFinishedDraft(result domain.HookRunResult, focus string) (RecordDraft, error) {
	payload := PreCompactHookFinishedPayload{Result: result}
	if focus != "" {
		evidence, err := newPreCompactFocusEvidence(focus, domain.MaxHookFeedbackBytes)
		if err != nil {
			return RecordDraft{}, err
		}
		payload.Focus = &evidence
	}
	if err := payload.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventPreCompactHookFinished, "", "", payload)
}

// NewPreCompactHookDecidedDraft 用空失败码与 ok 区分允许，其余结果必须封闭分类。
func NewPreCompactHookDecidedDraft(id domain.CompactionID, scope domain.CompactionScope, plan PreCompactPlan, finished int, code protocol.CompactionFailureCode, cause domain.HookCode) (RecordDraft, error) {
	target, err := domain.NewPreCompactHookTarget(id, scope)
	if err != nil {
		return RecordDraft{}, err
	}
	if plan.kind != preCompactPlanned || finished > len(plan.checks) {
		return RecordDraft{}, fmt.Errorf("invalid pre-compact decision plan or count")
	}
	payload := PreCompactHookDecidedPayload{Target: target, PlanDigest: plan.digest, Finished: finished, Code: code, Cause: cause}
	if err := payload.Validate(); err != nil {
		return RecordDraft{}, err
	}
	return encodeDraft(EventPreCompactHookDecided, "", "", payload)
}

// Validate 检查该结果可由当前 PreCompact runner 的正常分类产生。
func (payload PreCompactHookFinishedPayload) Validate() error {
	result := payload.Result
	if result.Validate() != nil || result.Identity.Event != domain.HookPreCompact || result.Origin != "execution" {
		return fmt.Errorf("invalid pre-compact execution result")
	}
	boundedOutput := result.StdoutBytes <= domain.MaxHookOutputBytes && result.StderrBytes <= domain.MaxHookOutputBytes
	cleanExit := result.ExitKnown && result.CleanupConfirmed && !result.Cancelled && !result.TimedOut && boundedOutput
	cleanExitZero := cleanExit && result.ExitCode == 0
	switch result.Code {
	case domain.HookOK, domain.HookDenied, domain.HookOutputInvalid, domain.HookFeedbackNotAllowed:
		if !cleanExitZero {
			return fmt.Errorf("invalid pre-compact parsed outcome")
		}
	case domain.HookExitFailed:
		if !cleanExit {
			return fmt.Errorf("invalid pre-compact exit failure")
		}
	case domain.HookOutputLimit:
		if boundedOutput {
			return fmt.Errorf("invalid pre-compact output-limit failure")
		}
	case domain.HookFeedbackBudgetExceeded:
		// 该原因只由完整请求预检产生，单项超限属于输出无效。
		return fmt.Errorf("pre-compact request budget is not an execution result")
	case domain.HookOutcomeUncertain:
		if result.CleanupConfirmed {
			return fmt.Errorf("invalid pre-compact uncertain outcome")
		}
	}
	if payload.Focus != nil && (!cleanExitZero || result.Status != domain.HookContinue || !payload.Focus.valid(1, domain.MaxHookFeedbackBytes) || int64(payload.Focus.Bytes) > result.StdoutBytes) {
		return fmt.Errorf("invalid pre-compact focus evidence")
	}
	return nil
}

// Validate 限定终态与安全原因的组合，运行前缀在回放时交叉验证。
func (payload PreCompactHookDecidedPayload) Validate() error {
	if !payload.Target.ValidFor(domain.HookPreCompact) || !domain.ValidHookDigest(payload.PlanDigest) || payload.Finished < 0 || payload.Finished > domain.MaxHookMatches {
		return fmt.Errorf("invalid pre-compact decision identity or count")
	}
	if !validPreCompactDecisionCause(payload.Code, payload.Cause) {
		return fmt.Errorf("invalid pre-compact decision cause")
	}
	return nil
}

func validPreCompactDecisionCause(code protocol.CompactionFailureCode, cause domain.HookCode) bool {
	switch code {
	case "":
		return cause == domain.HookOK
	case protocol.CompactionFailureHookTrustRequired:
		return cause == domain.HookTrustRequired
	case protocol.CompactionFailureHookTrustDenied:
		return cause == domain.HookTrustDenied
	case protocol.CompactionFailureHookDenied:
		return cause == domain.HookDenied
	case protocol.CompactionFailureHookFailed:
		return cause == domain.HookExecutionBudgetExhausted || (domain.HookError.ValidCode(cause) && cause != domain.HookConfigInvalid && cause != domain.HookMatchLimit)
	case protocol.CompactionFailureBudget, protocol.CompactionFailureUnknownBudget:
		return cause == domain.HookFeedbackBudgetExceeded
	case protocol.CompactionFailureCancelled:
		return cause == domain.HookCancelled
	case protocol.CompactionFailureSession:
		return cause == domain.HookOutcomeUncertain
	default:
		return false
	}
}

// DecodePreCompactHookStartedPayload 验证独立检查的准入身份。
func DecodePreCompactHookStartedPayload(record Record) (domain.HookRunIdentity, error) {
	payload, err := decodeKnownPayload[domain.HookRunIdentity](record, EventPreCompactHookStarted)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" || record.ParentThreadID != "" || payload.Event != domain.HookPreCompact || payload.Validate() != nil {
		return payload, fmt.Errorf("invalid pre-compact execution admission")
	}
	return payload, nil
}

// DecodePreCompactHookFinishedPayload 严格读取真实执行结论。
func DecodePreCompactHookFinishedPayload(record Record) (PreCompactHookFinishedPayload, error) {
	payload, err := decodeKnownPayload[PreCompactHookFinishedPayload](record, EventPreCompactHookFinished)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" || record.ParentThreadID != "" {
		return payload, fmt.Errorf("invalid pre-compact execution record scope")
	}
	return payload, payload.Validate()
}

// DecodePreCompactHookDecidedPayload 严格读取当前检查决定。
func DecodePreCompactHookDecidedPayload(record Record) (PreCompactHookDecidedPayload, error) {
	payload, err := decodeKnownPayload[PreCompactHookDecidedPayload](record, EventPreCompactHookDecided)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" || record.ParentThreadID != "" {
		return payload, fmt.Errorf("invalid pre-compact decision record scope")
	}
	return payload, payload.Validate()
}

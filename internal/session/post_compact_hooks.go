package session

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
)

// PostCompactPlanKind 表示成功检查点上的后置选择。
type PostCompactPlanKind string

const (
	PostCompactPlanNone    PostCompactPlanKind = "none"
	PostCompactPlanSkipped PostCompactPlanKind = "skipped"
	PostCompactPlanPlanned PostCompactPlanKind = "planned"
)

// PostCompactPlan 将有序声明的安全引用绑定到同一条已完成的检查点。
type PostCompactPlan struct {
	kind   PostCompactPlanKind
	code   domain.HookCode
	checks []compactionHookReference
	digest string
}

type postCompactNoneWire struct {
	Kind PostCompactPlanKind `json:"kind"`
}

type postCompactSkippedWire struct {
	Kind PostCompactPlanKind `json:"kind"`
	Code domain.HookCode     `json:"code"`
}

type postCompactPlannedWire struct {
	Kind   PostCompactPlanKind       `json:"kind"`
	Checks []compactionHookReference `json:"checks"`
	Digest string                    `json:"plan_digest"`
}

// NewNoPostCompactPlan 表示有效来源没有匹配的检查。
func NewNoPostCompactPlan() PostCompactPlan { return PostCompactPlan{kind: PostCompactPlanNone} }

// NewSkippedPostCompactPlan 记录计划创建时可确定的跳过原因。
func NewSkippedPostCompactPlan(code domain.HookCode) (PostCompactPlan, error) {
	switch code {
	case domain.HookTrustRequired, domain.HookMatchLimit, domain.HookPlanLimit:
		return PostCompactPlan{kind: PostCompactPlanSkipped, code: code}, nil
	default:
		return PostCompactPlan{}, fmt.Errorf("invalid post-compact plan skip reason")
	}
}

// NewPostCompactPlan 冻结匹配顺序，并使用实际候选检查点计算计划摘要。
func NewPostCompactPlan(checkpoint provider.CheckpointEnvelope, declarations []domain.HookDeclaration) (PostCompactPlan, error) {
	checks, err := freezeCompactionHookReferences(domain.HookPostCompact, checkpoint.Scope(), declarations)
	if err != nil {
		return PostCompactPlan{}, err
	}
	plan := PostCompactPlan{kind: PostCompactPlanPlanned, checks: checks}
	plan.digest, err = plan.computeDigest(checkpoint)
	if err != nil {
		return PostCompactPlan{}, err
	}
	return plan, nil
}

func (plan PostCompactPlan) Kind() PostCompactPlanKind { return plan.kind }
func (plan PostCompactPlan) Code() domain.HookCode     { return plan.code }
func (plan PostCompactPlan) Count() int                { return len(plan.checks) }
func (plan PostCompactPlan) Digest() string            { return plan.digest }

func (plan PostCompactPlan) computeDigest(checkpoint provider.CheckpointEnvelope) (string, error) {
	checkpointBytes, err := codec.MarshalStable(checkpoint)
	if err != nil {
		return "", err
	}
	b, err := codec.MarshalStable(struct {
		Event        domain.HookEvent          `json:"event"`
		ID           domain.CompactionID       `json:"compaction_id"`
		Scope        domain.CompactionScope    `json:"scope"`
		Checkpoint   string                    `json:"checkpoint_digest"`
		Declarations []compactionHookReference `json:"checks"`
	}{domain.HookPostCompact, checkpoint.ID(), checkpoint.Scope(), domain.HookDigest(checkpointBytes), plan.checks})
	if err != nil {
		return "", err
	}
	return domain.HookDigest(b), nil
}

func (plan PostCompactPlan) validate(checkpoint provider.CheckpointEnvelope) error {
	switch plan.kind {
	case PostCompactPlanNone:
		return nil
	case PostCompactPlanSkipped:
		_, err := NewSkippedPostCompactPlan(plan.code)
		return err
	case PostCompactPlanPlanned:
		if err := validateCompactionHookReferences(domain.HookPostCompact, checkpoint.Scope(), plan.checks); err != nil {
			return err
		}
		digest, err := plan.computeDigest(checkpoint)
		if err != nil {
			return err
		}
		if digest == plan.digest {
			return nil
		}
	}
	return fmt.Errorf("invalid post-compact plan")
}

// MarshalJSON 只编码当前互斥分支的字段。
func (plan PostCompactPlan) MarshalJSON() ([]byte, error) {
	switch plan.kind {
	case PostCompactPlanNone:
		return codec.MarshalStable(postCompactNoneWire{plan.kind})
	case PostCompactPlanSkipped:
		return codec.MarshalStable(postCompactSkippedWire{plan.kind, plan.code})
	case PostCompactPlanPlanned:
		return codec.MarshalStable(postCompactPlannedWire{plan.kind, plan.checks, plan.digest})
	default:
		return nil, fmt.Errorf("invalid post-compact plan kind")
	}
}

// UnmarshalJSON 拒绝无关分支字段；完成记录另行验证实际检查点。
func (plan *PostCompactPlan) UnmarshalJSON(data []byte) error {
	members, err := codec.ObjectMembers(data)
	if err != nil {
		return err
	}
	var kind PostCompactPlanKind
	if err := codec.Unmarshal(members["kind"], &kind); err != nil {
		return fmt.Errorf("invalid post-compact plan kind")
	}
	switch kind {
	case PostCompactPlanNone:
		var wire postCompactNoneWire
		if err := codec.UnmarshalRecord(data, &wire); err != nil {
			return err
		}
		*plan = NewNoPostCompactPlan()
	case PostCompactPlanSkipped:
		var wire postCompactSkippedWire
		if err := codec.UnmarshalRecord(data, &wire); err != nil {
			return err
		}
		value, err := NewSkippedPostCompactPlan(wire.Code)
		if err != nil {
			return err
		}
		*plan = value
	case PostCompactPlanPlanned:
		var wire postCompactPlannedWire
		if err := codec.UnmarshalRecord(data, &wire); err != nil {
			return err
		}
		*plan = PostCompactPlan{kind: kind, checks: wire.Checks, digest: wire.Digest}
	default:
		return fmt.Errorf("invalid post-compact plan kind")
	}
	return nil
}

type postCompactCloseOrigin string

const (
	postCompactLive     postCompactCloseOrigin = "live"
	postCompactRecovery postCompactCloseOrigin = "recovery"
)

// PostCompactHookClosedPayload 保存本地检查的最终安全结论及真实执行数量。
type PostCompactHookClosedPayload struct {
	Target     domain.HookTarget          `json:"target"`
	PlanDigest string                     `json:"plan_digest"`
	Finished   int                        `json:"finished"`
	State      protocol.PostCompactStatus `json:"state"`
	Code       domain.HookCode            `json:"code"`
	Origin     postCompactCloseOrigin     `json:"origin"`
}

// Validate 校验关闭事实自身身份与状态，回放另行核对相邻前缀。
func (payload PostCompactHookClosedPayload) Validate() error {
	if !payload.Target.ValidFor(domain.HookPostCompact) || !domain.ValidHookDigest(payload.PlanDigest) ||
		payload.Finished < 0 || payload.Finished > domain.MaxHookMatches || !payload.State.ValidCode(payload.Code) ||
		(payload.Origin != postCompactLive && payload.Origin != postCompactRecovery) ||
		(payload.State == protocol.PostCompactInterrupted && payload.Origin != postCompactRecovery) {
		return fmt.Errorf("invalid post-compact closure")
	}
	return nil
}

// NewPostCompactHookStartedDraft 保存 runner 接纳前的单项执行身份。
func NewPostCompactHookStartedDraft(id domain.HookRunIdentity) (RecordDraft, error) {
	if id.Event != domain.HookPostCompact || id.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid post-compact execution admission")
	}
	return encodeDraft(EventPostCompactHookStarted, "", "", id)
}

// NewPostCompactHookFinishedDraft 保存单项实际执行统计及结论。
func NewPostCompactHookFinishedDraft(result domain.HookRunResult) (RecordDraft, error) {
	if result.Identity.Event != domain.HookPostCompact || result.Validate() != nil {
		return RecordDraft{}, fmt.Errorf("invalid post-compact execution result")
	}
	return encodeDraft(EventPostCompactHookFinished, "", "", result)
}

// NewPostCompactHookClosedDraft 根据实际前缀和实时停止原因构造唯一关闭事实。
func NewPostCompactHookClosedDraft(target domain.HookTarget, plan PostCompactPlan, finished []domain.HookRunResult, liveCause domain.HookCode) (RecordDraft, PostCompactHookClosedPayload, error) {
	state := postCompactReplay{target: target, plan: plan}
	for _, result := range finished {
		if !state.acceptsNext(result.Identity) || result.Validate() != nil {
			return RecordDraft{}, PostCompactHookClosedPayload{}, fmt.Errorf("invalid post-compact execution prefix")
		}
		state.finished = append(state.finished, result)
	}
	payload, err := state.closed(postCompactLive, liveCause)
	if err != nil {
		return RecordDraft{}, PostCompactHookClosedPayload{}, err
	}
	draft, err := encodeDraft(EventPostCompactHookClosed, "", "", payload)
	return draft, payload, err
}

// DecodePostCompactHookStartedPayload 严格读取独立后置检查的执行准入。
func DecodePostCompactHookStartedPayload(record Record) (domain.HookRunIdentity, error) {
	payload, err := decodeKnownPayload[domain.HookRunIdentity](record, EventPostCompactHookStarted)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" || record.ParentThreadID != "" || payload.Event != domain.HookPostCompact || payload.Validate() != nil {
		return payload, fmt.Errorf("invalid post-compact execution admission")
	}
	return payload, nil
}

// DecodePostCompactHookFinishedPayload 严格读取真实执行结论。
func DecodePostCompactHookFinishedPayload(record Record) (domain.HookRunResult, error) {
	payload, err := decodeKnownPayload[domain.HookRunResult](record, EventPostCompactHookFinished)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" || record.ParentThreadID != "" || payload.Identity.Event != domain.HookPostCompact || payload.Validate() != nil {
		return payload, fmt.Errorf("invalid post-compact execution result")
	}
	return payload, nil
}

// DecodePostCompactHookClosedPayload 严格读取后置检查的合并关闭事实。
func DecodePostCompactHookClosedPayload(record Record) (PostCompactHookClosedPayload, error) {
	payload, err := decodeKnownPayload[PostCompactHookClosedPayload](record, EventPostCompactHookClosed)
	if err != nil {
		return payload, err
	}
	if record.TurnID != "" || record.ParentThreadID != "" {
		return payload, fmt.Errorf("invalid post-compact closure scope")
	}
	return payload, payload.Validate()
}

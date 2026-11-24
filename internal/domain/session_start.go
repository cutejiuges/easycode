package domain

import (
	"slices"
	"strconv"
	"strings"

	"easycode/internal/codec"
)

// SessionStartSource 是可派发的实际会话事件来源。
type SessionStartSource string

const (
	SessionStartStartup   SessionStartSource = "startup"
	SessionStartResume    SessionStartSource = "resume"
	SessionStartCompact   SessionStartSource = "compact"
	anySessionStartSource SessionStartSource = "*"
)

func (source SessionStartSource) Valid() bool {
	return source == SessionStartStartup || source == SessionStartResume || source == SessionStartCompact
}

// ValidateSessionStartReferences 校验同一冻结快照的会话 Hook 引用顺序与唯一性。
func ValidateSessionStartReferences(refs []HookReference) error {
	return validateHookReferences(HookSessionStart, refs)
}

// SessionStartTarget 关联真实首轮或一个已安装检查点。压缩回合仅由 Scope 表达。
type SessionStartTarget struct {
	Source             SessionStartSource
	TurnID             TurnID
	CompactionID       CompactionID
	Scope              CompactionScope
	CheckpointSequence uint64
	CheckpointRevision uint64
	CheckpointDigest   string
}

func (v SessionStartTarget) Validate() error {
	switch v.Source {
	case SessionStartStartup, SessionStartResume:
		if v.TurnID.Valid() && v.CompactionID == "" && v.Scope == (CompactionScope{}) &&
			v.CheckpointSequence == 0 && v.CheckpointRevision == 0 && v.CheckpointDigest == "" {
			return nil
		}
	case SessionStartCompact:
		if v.TurnID == "" && v.CompactionID.Valid() && v.Scope.Validate() == nil &&
			v.CheckpointSequence > 0 && v.CheckpointRevision > 0 && ValidHookDigest(v.CheckpointDigest) {
			return nil
		}
	}
	return HookConfigInvalid
}

func (v SessionStartTarget) key() string {
	if v.Source != SessionStartCompact {
		return string(v.Source) + ":" + string(v.TurnID)
	}
	return strings.Join([]string{
		string(v.Source), string(v.CompactionID), string(v.Scope.Trigger), string(v.Scope.TurnID),
		strconv.FormatUint(uint64(v.Scope.SampleIndex), 10), strconv.FormatUint(v.CheckpointSequence, 10),
		strconv.FormatUint(v.CheckpointRevision, 10), v.CheckpointDigest,
	}, ":")
}

func (v SessionStartTarget) turn() TurnID {
	if v.Source == SessionStartCompact {
		return v.Scope.TurnID
	}
	return v.TurnID
}

type sessionStartActivationTargetWire struct {
	Source SessionStartSource `json:"source"`
	TurnID TurnID             `json:"turn_id"`
}

type sessionStartCompactionTargetWire struct {
	Source             SessionStartSource `json:"source"`
	CompactionID       CompactionID       `json:"compaction_id"`
	Scope              CompactionScope    `json:"scope"`
	CheckpointSequence uint64             `json:"checkpoint_sequence"`
	CheckpointRevision uint64             `json:"checkpoint_revision"`
	CheckpointDigest   string             `json:"checkpoint_digest"`
}

// MarshalJSON 按实际来源保存唯一匹配的目标分支。
func (v SessionStartTarget) MarshalJSON() ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	if v.Source != SessionStartCompact {
		return codec.MarshalStable(sessionStartActivationTargetWire{v.Source, v.TurnID})
	}
	return codec.MarshalStable(sessionStartCompactionTargetWire{
		v.Source, v.CompactionID, v.Scope, v.CheckpointSequence, v.CheckpointRevision, v.CheckpointDigest,
	})
}

// UnmarshalJSON 校验来源、字段存在性与目标分支。
func (v *SessionStartTarget) UnmarshalJSON(data []byte) error {
	fields, err := codec.ObjectMembers(data)
	if err != nil {
		return HookConfigInvalid
	}
	var source SessionStartSource
	if codec.UnmarshalRecord(fields["source"], &source) != nil {
		return HookConfigInvalid
	}
	var target SessionStartTarget
	if source == SessionStartCompact {
		var w sessionStartCompactionTargetWire
		if codec.UnmarshalRecord(data, &w) != nil {
			return HookConfigInvalid
		}
		target = SessionStartTarget{
			Source: w.Source, CompactionID: w.CompactionID, Scope: w.Scope, CheckpointSequence: w.CheckpointSequence,
			CheckpointRevision: w.CheckpointRevision, CheckpointDigest: w.CheckpointDigest,
		}
	} else {
		var w sessionStartActivationTargetWire
		if codec.UnmarshalRecord(data, &w) != nil {
			return HookConfigInvalid
		}
		target = SessionStartTarget{Source: w.Source, TurnID: w.TurnID}
	}
	if err := target.Validate(); err != nil {
		return err
	}
	*v = target
	return nil
}

const (
	MaxSessionStartContextBytes = 16 << 10
	sessionStartContextPrefix   = "<session_start_context>\nLocal hook context follows. Existing user instructions and tool permissions still apply.\n"
	sessionStartContextSuffix   = "\n</session_start_context>"
	// 单项证明在总量裁决前形成，允许合法正文转义后超过整个交付额度。
	maxSessionStartProofBytes = MaxHookFeedbackBytes*6 + len(sessionStartContextPrefix) + len(sessionStartContextSuffix) + len(`{"source":"startup","context":""}`)
)

type sessionStartContextBody struct {
	Source  SessionStartSource `json:"source"`
	Context string             `json:"context"`
}

// WrapSessionStartContext 把已认可的文本按来源包装为用户层数据，保留原文空白。
func WrapSessionStartContext(source SessionStartSource, body string) (string, error) {
	if !source.Valid() {
		return "", HookConfigInvalid
	}
	if _, err := NewHookFeedbackCandidate(body); err != nil {
		return "", err
	}
	data, err := codec.MarshalStable(sessionStartContextBody{source, body})
	if err != nil {
		return "", HookFeedbackEncodingFailed
	}
	return sessionStartContextPrefix + string(data) + sessionStartContextSuffix, nil
}

// SessionStartProof 记录实际模型包装的字节数与摘要，不包含正文。
type SessionStartProof struct {
	Ordinal   int           `json:"ordinal"`
	Reference HookReference `json:"reference"`
	Bytes     int           `json:"bytes"`
	Digest    string        `json:"digest"`
}

func (v SessionStartProof) Validate() error {
	if v.Ordinal < 0 || v.Ordinal >= MaxHookMatches || v.Reference.Validate() != nil ||
		v.Reference.Event != HookSessionStart || v.Reference.Feedback != HookFeedbackModel ||
		v.Bytes <= 0 || v.Bytes > maxSessionStartProofBytes || !ValidHookDigest(v.Digest) {
		return HookConfigInvalid
	}
	return nil
}

// UnmarshalJSON 严格要求零合法的 ordinal 也显式编码。
func (v *SessionStartProof) UnmarshalJSON(data []byte) error {
	type wire SessionStartProof
	var decoded wire
	if codec.UnmarshalRecord(data, &decoded) != nil {
		return HookConfigInvalid
	}
	proof := SessionStartProof(decoded)
	if err := proof.Validate(); err != nil {
		return err
	}
	*v = proof
	return nil
}

// SessionStartContextItem 保留一项顺序确定的已包装模型文本及其来源。
type SessionStartContextItem struct {
	Ordinal   int
	Reference HookReference
	Text      string
}

func (v SessionStartContextItem) Proof() SessionStartProof {
	return SessionStartProof{Ordinal: v.Ordinal, Reference: v.Reference, Bytes: len(v.Text), Digest: HookDigest([]byte(v.Text))}
}

// SessionStartContext 是原生提交所持有的获准上下文，不表示真实用户输入。
type SessionStartContext struct {
	Target SessionStartTarget
	Items  []SessionStartContextItem
}

func (v SessionStartContext) Clone() SessionStartContext {
	v.Items = slices.Clone(v.Items)
	return v
}

func (v SessionStartContext) Validate() error {
	if v.Target.Validate() != nil || len(v.Items) == 0 || len(v.Items) > MaxHookMatches {
		return HookConfigInvalid
	}
	total, previous := 0, -1
	refs := make([]HookReference, 0, len(v.Items))
	for _, item := range v.Items {
		if item.Ordinal <= previous || len(item.Text) > MaxSessionStartContextBytes-total ||
			item.Proof().Validate() != nil || !validSessionStartContextText(v.Target.Source, item.Text) {
			return HookConfigInvalid
		}
		previous = item.Ordinal
		total += len(item.Text)
		refs = append(refs, item.Reference)
	}
	return ValidateSessionStartReferences(refs)
}

func validSessionStartContextText(source SessionStartSource, text string) bool {
	body, ok := strings.CutPrefix(text, sessionStartContextPrefix)
	if !ok {
		return false
	}
	body, ok = strings.CutSuffix(body, sessionStartContextSuffix)
	if !ok {
		return false
	}
	var value sessionStartContextBody
	if codec.UnmarshalRecord([]byte(body), &value) != nil || value.Source != source {
		return false
	}
	canonical, err := WrapSessionStartContext(source, value.Context)
	return err == nil && canonical == text
}

func (r HookRunResult) validSessionStartResult() bool {
	const overflowBytes = MaxHookOutputBytes + 1
	if r.StdoutBytes > overflowBytes || r.StderrBytes > overflowBytes {
		return false
	}
	overflow := r.StdoutBytes == overflowBytes || r.StderrBytes == overflowBytes
	switch r.Code {
	case HookOutcomeUncertain:
		return !r.CleanupConfirmed
	case HookCancelled, HookTimedOut:
		return true
	case HookOutputLimit:
		return overflow
	case HookOutputInvalid, HookFeedbackNotAllowed:
		return !overflow && r.ExitKnown && r.ExitCode == 0
	case HookOK, HookExitFailed, HookStartFailed, HookInputLimit, HookIOFailed, HookUnsupported, HookSandboxUnavailable:
		return !overflow
	default:
		return false
	}
}

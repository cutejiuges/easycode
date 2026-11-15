package protocol

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// PreInputKind 区分已保存的检查候选与 gate 最终选择，不能表示执行准入。
type PreInputKind string

const (
	PreInputNone      PreInputKind = "none"
	PreInputCandidate PreInputKind = "candidate"
	PreInputSelected  PreInputKind = "selected"
	PreInputRejected  PreInputKind = "rejected"
)

// PreInputProjection 只保存有界摘要；完整输入由内存详情端口分页提供。
type PreInputProjection struct {
	kind     PreInputKind
	sequence uint64
	digest   string
	summary  string
}

func NewPreInputProjection(kind PreInputKind, sequence uint64, digest, summary string) (PreInputProjection, error) {
	v := PreInputProjection{kind, sequence, digest, summary}
	return v, v.Validate()
}
func (v PreInputProjection) Kind() PreInputKind {
	if v.kind == "" {
		return PreInputNone
	}
	return v.kind
}
func (v PreInputProjection) Sequence() uint64 { return v.sequence }
func (v PreInputProjection) Digest() string   { return v.digest }
func (v PreInputProjection) Summary() string  { return v.summary }
func (v PreInputProjection) Validate() error {
	if v.Kind() == PreInputNone {
		if v.sequence != 0 || v.digest != "" || v.summary != "" {
			return fmt.Errorf("invalid empty pre input projection")
		}
		return nil
	}
	if v.kind != PreInputCandidate && v.kind != PreInputSelected && v.kind != PreInputRejected || v.sequence == 0 || !domain.ValidHookDigest(v.digest) || v.summary == "" || !utf8.ValidString(v.summary) || utf8.RuneCountInString(v.summary) > 160 || strings.ContainsAny(v.summary, "\r\n") {
		return fmt.Errorf("invalid pre input projection")
	}
	encoded, err := v.MarshalJSON()
	if err != nil || len(encoded) > 4<<10 {
		return fmt.Errorf("pre input projection exceeds limit")
	}
	return nil
}
func (v PreInputProjection) MarshalJSON() ([]byte, error) {
	if v.Kind() == PreInputNone {
		return []byte(`{"kind":"none"}`), nil
	}
	return codec.MarshalStable(struct {
		Kind     PreInputKind `json:"kind"`
		Sequence uint64       `json:"detail_sequence"`
		Digest   string       `json:"input_digest"`
		Summary  string       `json:"summary"`
	}{v.kind, v.sequence, v.digest, v.summary})
}
func (v *PreInputProjection) UnmarshalJSON(data []byte) error {
	if len(data) > 4<<10 {
		return fmt.Errorf("pre input projection exceeds limit")
	}
	members, err := codec.ObjectMembers(data)
	if err != nil {
		return err
	}
	var kind PreInputKind
	if codec.UnmarshalRecord(members["kind"], &kind) != nil {
		return fmt.Errorf("invalid pre input projection kind")
	}
	if kind == PreInputNone {
		if len(members) != 1 {
			return fmt.Errorf("invalid empty pre input projection")
		}
		*v = PreInputProjection{}
		return nil
	}
	var wire struct {
		Kind     PreInputKind `json:"kind"`
		Sequence uint64       `json:"detail_sequence"`
		Digest   string       `json:"input_digest"`
		Summary  string       `json:"summary"`
	}
	if err := codec.UnmarshalRecord(data, &wire); err != nil {
		return err
	}
	value, err := NewPreInputProjection(wire.Kind, wire.Sequence, wire.Digest, wire.Summary)
	if err == nil {
		*v = value
	}
	return err
}

// hookActivityWire 的 Pre 分支为必需成员，其他事件严格禁止携带。
type hookActivityWire struct {
	Target      domain.HookTarget    `json:"target"`
	PlanDigest  string               `json:"plan_digest"`
	Reference   domain.HookReference `json:"reference"`
	State       HookActivityState    `json:"state"`
	Code        domain.HookCode      `json:"code"`
	StdoutBytes int64                `json:"stdout_bytes"`
	StderrBytes int64                `json:"stderr_bytes"`
	PreInput    *PreInputProjection  `json:"pre_input,omitempty"`
}

func (p HookActivityPayload) MarshalJSON() ([]byte, error) {
	if p.Reference.Event != domain.HookPreToolUse && p.PreInput != (PreInputProjection{}) {
		return nil, fmt.Errorf("unrelated pre input projection")
	}
	w := hookActivityWire{Target: p.Target, PlanDigest: p.PlanDigest, Reference: p.Reference, State: p.State, Code: p.Code, StdoutBytes: p.StdoutBytes, StderrBytes: p.StderrBytes}
	if p.Reference.Event == domain.HookPreToolUse {
		value := p.PreInput
		w.PreInput = &value
	}
	return codec.MarshalStable(w)
}
func (p *HookActivityPayload) UnmarshalJSON(data []byte) error {
	members, err := codec.ObjectMembers(data)
	if err != nil {
		return err
	}
	var w hookActivityWire
	if err := codec.UnmarshalRecord(data, &w); err != nil {
		return err
	}
	_, present := members["pre_input"]
	if (w.Reference.Event == domain.HookPreToolUse) != present || present && w.PreInput == nil {
		return fmt.Errorf("invalid hook pre input branch")
	}
	value := HookActivityPayload{Target: w.Target, PlanDigest: w.PlanDigest, Reference: w.Reference, State: w.State, Code: w.Code, StdoutBytes: w.StdoutBytes, StderrBytes: w.StderrBytes}
	if w.PreInput != nil {
		value.PreInput = *w.PreInput
	}
	if err := value.Validate(); err != nil {
		return err
	}
	*p = value
	return nil
}

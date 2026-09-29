// Package context 负责上下文分段、稳定序列化和缓存指纹。
package context

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"

	"easycode/internal/codec"
)

const (
	CurrentPlanVersion = 1
	// MaxSegmentBytes 限制单个缓存分段持有的 canonical JSON 大小。
	MaxSegmentBytes = 16 << 20
)

// Stability 描述缓存分段在生命周期中的稳定级别。
type Stability string

const (
	StabilityStable        Stability = "stable"
	StabilityProjectStable Stability = "project_stable"
	StabilitySessionStable Stability = "session_stable"
	StabilityTurnStable    Stability = "turn_stable"
	StabilityVolatile      Stability = "volatile"
)

// Segment 保存一段可独立诊断和失效的上下文。
type Segment struct {
	id            string
	stability     Stability
	revision      string
	fingerprint   string
	canonicalJSON codec.CanonicalJSON
}

// NewSegment 使用已校验的 canonical JSON 生成不可变分段和 SHA-256 指纹。
func NewSegment(id string, stability Stability, revision string, content codec.CanonicalJSON) (Segment, error) {
	if strings.TrimSpace(id) == "" {
		return Segment{}, fmt.Errorf("segment ID is required")
	}
	if !isKnownStability(stability) {
		return Segment{}, fmt.Errorf("cache segment %q has unknown stability %q", id, stability)
	}
	if strings.TrimSpace(revision) == "" {
		return Segment{}, fmt.Errorf("cache segment %q revision is required", id)
	}
	if err := content.Validate(MaxSegmentBytes); err != nil {
		return Segment{}, fmt.Errorf("cache segment %q content is invalid: %w", id, err)
	}
	canonical := content.Clone()
	sum := sha256.Sum256(canonical.Bytes())
	return Segment{
		id:            id,
		stability:     stability,
		revision:      revision,
		fingerprint:   hex.EncodeToString(sum[:]),
		canonicalJSON: canonical,
	}, nil
}

// ID 返回分段稳定标识。
func (segment Segment) ID() string { return segment.id }

// Stability 返回分段稳定级别。
func (segment Segment) Stability() Stability { return segment.stability }

// Revision 返回分段来源 revision。
func (segment Segment) Revision() string { return segment.revision }

// Fingerprint 返回分段内容的 SHA-256 指纹。
func (segment Segment) Fingerprint() string { return segment.fingerprint }

// CanonicalJSON 返回分段 canonical JSON 的独立副本。
func (segment Segment) CanonicalJSON() []byte { return segment.canonicalJSON.Bytes() }

func (segment Segment) clone() Segment {
	segment.canonicalJSON = segment.canonicalJSON.Clone()
	return segment
}

func (segment Segment) validate() error {
	if strings.TrimSpace(segment.id) == "" {
		return fmt.Errorf("segment ID is required")
	}
	if !isKnownStability(segment.stability) {
		return fmt.Errorf("cache segment %q has unknown stability %q", segment.id, segment.stability)
	}
	if strings.TrimSpace(segment.revision) == "" {
		return fmt.Errorf("cache segment %q revision is required", segment.id)
	}
	if err := segment.canonicalJSON.Validate(MaxSegmentBytes); err != nil {
		return fmt.Errorf("cache segment %q content is invalid: %w", segment.id, err)
	}
	sum := sha256.Sum256(segment.canonicalJSON.Bytes())
	if segment.fingerprint != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("cache segment %q fingerprint does not match content", segment.id)
	}
	return nil
}

// Plan 是 Provider CachePlanner 的有序输入。
type Plan struct {
	version  int
	segments []Segment
}

// NewPlan 创建当前版本的缓存计划并复制输入切片。
func NewPlan(segments ...Segment) (Plan, error) {
	return newPlan(CurrentPlanVersion, segments)
}

func newPlan(version int, segments []Segment) (Plan, error) {
	plan := Plan{version: version, segments: cloneSegments(segments)}
	if err := plan.Validate(); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// Validate 检查分段唯一性和稳定前缀顺序。
func (plan Plan) Validate() error {
	if plan.version != CurrentPlanVersion {
		return fmt.Errorf("unsupported cache plan version %d", plan.version)
	}
	seen := make(map[string]struct{}, len(plan.segments))
	volatileSeen := false
	for _, segment := range plan.segments {
		if err := segment.validate(); err != nil {
			return err
		}
		if _, exists := seen[segment.id]; exists {
			return fmt.Errorf("duplicate cache segment ID %q", segment.id)
		}
		seen[segment.id] = struct{}{}
		if !isPrefixStable(segment.stability) {
			volatileSeen = true
			continue
		}
		if volatileSeen {
			return fmt.Errorf("stable cache segment %q appears after a volatile segment", segment.id)
		}
	}
	return nil
}

// Version 返回缓存计划版本。
func (plan Plan) Version() int { return plan.version }

// Segments 返回不与计划共享可变数据的有序分段副本。
func (plan Plan) Segments() []Segment { return cloneSegments(plan.segments) }

// StablePrefixFingerprint 返回只覆盖稳定前缀的组合指纹。
func (plan Plan) StablePrefixFingerprint() (string, error) {
	if err := plan.Validate(); err != nil {
		return "", err
	}
	digest := sha256.New()
	for _, segment := range plan.segments {
		if !isPrefixStable(segment.stability) {
			break
		}
		writeFingerprintPart(digest, segment.id)
		writeFingerprintPart(digest, segment.revision)
		writeFingerprintPart(digest, segment.fingerprint)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func isPrefixStable(stability Stability) bool {
	return stability == StabilityStable ||
		stability == StabilityProjectStable ||
		stability == StabilitySessionStable
}

func isKnownStability(stability Stability) bool {
	return isPrefixStable(stability) ||
		stability == StabilityTurnStable ||
		stability == StabilityVolatile
}

func cloneSegments(segments []Segment) []Segment {
	clones := make([]Segment, len(segments))
	for index := range segments {
		clones[index] = segments[index].clone()
	}
	return clones
}

func writeFingerprintPart(digest hash.Hash, value string) {
	_, _ = digest.Write([]byte(value))
	_, _ = digest.Write([]byte{0})
}

// Package context 负责上下文分段、稳定序列化和缓存指纹。
package context

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"

	"easycode/internal/codec"
)

const CurrentPlanVersion = 1

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
	ID            string    `json:"id"`
	Stability     Stability `json:"stability"`
	Revision      string    `json:"revision"`
	Fingerprint   string    `json:"fingerprint"`
	CanonicalJSON []byte    `json:"-"`
}

// NewSegment 使用 Sonic 稳定配置生成 canonical JSON 和 SHA-256 指纹。
func NewSegment(id string, stability Stability, revision string, value any) (Segment, error) {
	if id == "" {
		return Segment{}, fmt.Errorf("segment ID is required")
	}
	canonicalJSON, err := codec.MarshalStable(value)
	if err != nil {
		return Segment{}, fmt.Errorf("marshal cache segment %q: %w", id, err)
	}
	sum := sha256.Sum256(canonicalJSON)
	return Segment{
		ID:            id,
		Stability:     stability,
		Revision:      revision,
		Fingerprint:   hex.EncodeToString(sum[:]),
		CanonicalJSON: append([]byte(nil), canonicalJSON...),
	}, nil
}

// Plan 是 Provider CachePlanner 的有序输入。
type Plan struct {
	Version  int       `json:"version"`
	Segments []Segment `json:"segments"`
}

// NewPlan 创建当前版本的缓存计划并复制输入切片。
func NewPlan(segments ...Segment) Plan {
	return Plan{
		Version:  CurrentPlanVersion,
		Segments: append([]Segment(nil), segments...),
	}
}

// Validate 检查分段唯一性和稳定前缀顺序。
func (plan Plan) Validate() error {
	seen := make(map[string]struct{}, len(plan.Segments))
	volatileSeen := false
	for _, segment := range plan.Segments {
		if _, exists := seen[segment.ID]; exists {
			return fmt.Errorf("duplicate cache segment ID %q", segment.ID)
		}
		seen[segment.ID] = struct{}{}
		if !isPrefixStable(segment.Stability) {
			volatileSeen = true
			continue
		}
		if volatileSeen {
			return fmt.Errorf("stable cache segment %q appears after a volatile segment", segment.ID)
		}
	}
	return nil
}

// StablePrefixFingerprint 返回只覆盖稳定前缀的组合指纹。
func (plan Plan) StablePrefixFingerprint() (string, error) {
	if err := plan.Validate(); err != nil {
		return "", err
	}
	digest := sha256.New()
	for _, segment := range plan.Segments {
		if !isPrefixStable(segment.Stability) {
			break
		}
		writeFingerprintPart(digest, segment.ID)
		writeFingerprintPart(digest, segment.Revision)
		writeFingerprintPart(digest, segment.Fingerprint)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func isPrefixStable(stability Stability) bool {
	return stability == StabilityStable ||
		stability == StabilityProjectStable ||
		stability == StabilitySessionStable
}

func writeFingerprintPart(digest hash.Hash, value string) {
	_, _ = digest.Write([]byte(value))
	_, _ = digest.Write([]byte{0})
}

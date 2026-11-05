package context

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// CacheSourceStamp 是不含正文的单个固定来源证据。
type CacheSourceStamp struct {
	Kind        SourceKind `json:"kind"`
	Present     bool       `json:"present"`
	Revision    string     `json:"revision"`
	Fingerprint string     `json:"fingerprint"`
}

// CacheSources 固定保存当前六种来源，内存不随会话长度增长。
type CacheSources struct {
	Sources            [6]CacheSourceStamp `json:"sources"`
	StableFingerprint  string              `json:"stable_fingerprint"`
	ContentFingerprint string              `json:"content_fingerprint"`
}

func cacheSourceOrder() [6]SourceKind {
	return [6]SourceKind{SourceProviderProfile, SourceToolCatalog, SourceProjectInstructions, SourceSkillCatalog, SourceCommittedHistory, SourceCurrentInput}
}

// NewCacheSources 从已冻结计划提取有界来源，不保留 canonical 正文。
func NewCacheSources(plan ContextPlan) (CacheSources, error) {
	if err := plan.Validate(); err != nil {
		return CacheSources{}, err
	}
	result := CacheSources{}
	for i, kind := range cacheSourceOrder() {
		result.Sources[i].Kind = kind
		for _, segment := range plan.cachePlan.segments {
			if segment.ID() == string(kind) {
				result.Sources[i] = CacheSourceStamp{Kind: kind, Present: true, Revision: segment.Revision(), Fingerprint: segment.Fingerprint()}
				break
			}
		}
	}
	var err error
	result.StableFingerprint, err = plan.cachePlan.StablePrefixFingerprint()
	if err != nil {
		return CacheSources{}, err
	}
	raw, err := codec.MarshalCanonical(result.Sources, 4096)
	if err != nil {
		return CacheSources{}, err
	}
	result.ContentFingerprint = domain.ContentFingerprint(raw.Bytes())
	return result, result.Validate()
}

// Validate 拒绝无限来源、正文和不符合固定来源顺序的证据。
func (s CacheSources) Validate() error {
	for i, kind := range cacheSourceOrder() {
		source := s.Sources[i]
		if source.Kind != kind {
			return fmt.Errorf("cache source order is invalid")
		}
		if !source.Present {
			if (kind != SourceProjectInstructions && kind != SourceSkillCatalog) || source.Revision != "" || source.Fingerprint != "" {
				return fmt.Errorf("cache source is missing")
			}
			continue
		}
		if len(source.Revision) == 0 || len(source.Revision) > 128 || !domain.ValidFingerprint(source.Fingerprint) {
			return fmt.Errorf("cache source metadata is invalid")
		}
		for _, r := range source.Revision {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
				return fmt.Errorf("cache source revision is invalid")
			}
		}
	}
	raw, err := codec.MarshalCanonical(s.Sources, 4096)
	if err != nil {
		return err
	}
	if !domain.ValidFingerprint(s.StableFingerprint) || s.ContentFingerprint != domain.ContentFingerprint(raw.Bytes()) {
		return fmt.Errorf("cache source fingerprint is invalid")
	}
	return nil
}

// CacheChange 是封闭的首个来源变化解释。
type CacheChange struct {
	Reason string     `json:"reason"`
	Source SourceKind `json:"source"`
}

// CompareCacheSources 将内容变化与原生关系分开，不推测服务端命中。
func CompareCacheSources(previous, current CacheSources, priorNative string, receipt domain.RequestReceipt, restored bool) CacheChange {
	if receipt.Reason == domain.ResetCompacted {
		return CacheChange{Reason: "context_compacted"}
	}
	if restored || receipt.Reason == domain.ResetRestored {
		return CacheChange{Reason: "restored_baseline"}
	}
	if previous.ContentFingerprint == "" {
		return CacheChange{Reason: "first_request"}
	}
	for i, source := range current.Sources {
		old := previous.Sources[i]
		if source.Present != old.Present {
			if source.Present {
				return CacheChange{"source_added", source.Kind}
			}
			return CacheChange{"source_removed", source.Kind}
		}
		if source.Fingerprint != old.Fingerprint || source.Revision != old.Revision {
			if source.Kind == SourceCommittedHistory && receipt.Relation == domain.NativeAppend {
				return CacheChange{"history_appended", source.Kind}
			}
			if source.Fingerprint != old.Fingerprint {
				return CacheChange{"source_content_changed", source.Kind}
			}
			return CacheChange{"source_revision_changed", source.Kind}
		}
	}
	if priorNative != "" && priorNative != receipt.NativeFingerprint {
		if receipt.Relation == domain.NativeAppend {
			return CacheChange{"history_appended", SourceCommittedHistory}
		}
		return CacheChange{"native_context_changed", SourceCommittedHistory}
	}
	return CacheChange{Reason: "unchanged"}
}

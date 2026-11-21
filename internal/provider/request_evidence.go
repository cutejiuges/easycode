package provider

import (
	"encoding/hex"
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

// RequestEvidence 保存逻辑全量输入和请求属性，不包含传输派生的后缀和缓存标记。
type RequestEvidence struct {
	items             []codec.CanonicalJSON
	properties        codec.CanonicalJSON
	nativeFingerprint string
}

// RequestComparison 冻结一次提交基线比较，供传输选择和收据共用。
type RequestComparison struct {
	offset   int
	relation domain.NativeRelation
	reason   domain.RequestResetReason
}

// Suffix 返回可复用的前缀长度及重置原因。
func (c RequestComparison) Suffix() (int, domain.RequestResetReason) {
	return c.offset, c.reason
}

func canonicalRequestItems[E any](items []E) ([]codec.CanonicalJSON, error) {
	result := make([]codec.CanonicalJSON, 0, len(items))
	for _, item := range items {
		part, err := codec.MarshalCanonical(item, MaxNativeCommitBytes)
		if err != nil {
			return nil, err
		}
		result = append(result, part)
	}
	return result, nil
}

// CompileRequestEvidence 从原生输入与去掉该输入的请求属性提取逻辑证据。
func CompileRequestEvidence[E, P any](input []E, wireProperties P, inputField string, maxRequestBytes int) (RequestEvidence, error) {
	items, err := canonicalRequestItems(input)
	if err != nil {
		return RequestEvidence{}, err
	}
	native, err := codec.CanonicalArraySHA256(items, MaxNativeCommitBytes)
	if err != nil {
		return RequestEvidence{}, err
	}
	properties, err := codec.MarshalCanonical(wireProperties, maxRequestBytes)
	if err != nil {
		return RequestEvidence{}, err
	}
	members, err := codec.ObjectMembers(properties.Bytes())
	if err != nil {
		return RequestEvidence{}, err
	}
	delete(members, inputField)
	properties, err = codec.MarshalCanonical(members, maxRequestBytes)
	if err != nil {
		return RequestEvidence{}, err
	}
	return RequestEvidence{items, properties, hex.EncodeToString(native[:])}, nil
}

// Properties 返回已冻结的请求属性。
func (e RequestEvidence) Properties() codec.CanonicalJSON { return e.properties }

// NativeFingerprint 返回完整逻辑输入的指纹。
func (e RequestEvidence) NativeFingerprint() string { return e.nativeFingerprint }

// Compare 与已提交基线比较逻辑输入及原生请求属性。
func (e RequestEvidence) Compare(baseline RequestBaseline) RequestComparison {
	return baseline.Compare(e.items, e.properties)
}

// BaselineWithOutput 冻结逻辑输入和已完成的原生输出，供 durable 后安装。
func BaselineWithOutput[E any](e RequestEvidence, output []E) (RequestBaseline, error) {
	items, err := canonicalRequestItems(output)
	if err != nil {
		return RequestBaseline{}, err
	}
	return NewRequestBaseline(e.items, items, e.properties)
}

// Receipt 记录逻辑请求和窗口关系，传输策略可以在后续据实补充传输信息。
func (e RequestEvidence) Receipt(body codec.CanonicalJSON, comparison RequestComparison, nextReason domain.RequestResetReason, receipt domain.RequestReceipt) (domain.RequestReceipt, error) {
	relation, reason := comparison.relation, comparison.reason
	if nextReason != "" && nextReason != domain.ResetNone {
		reason = nextReason
		if reason == domain.ResetCompacted {
			relation = domain.NativeBoundary
		}
	}
	if receipt.Purpose == domain.RequestSummary {
		reason = domain.ResetSummary
		relation = domain.NativeBoundary
	}
	bodyDigest := body.SHA256()
	receipt.RequestFingerprint = hex.EncodeToString(bodyDigest[:])
	receipt.NativeFingerprint = e.nativeFingerprint
	receipt.Relation = relation
	receipt.Reason = reason
	receipt.Transport = domain.TransportHTTP
	receipt.Mode = domain.RequestFull
	receipt.Fallback = domain.FallbackNone
	return domain.NewRequestReceipt(receipt)
}

// SamplingSources 冻结当前操作共用的原生窗口与请求来源。
type SamplingSources[E any] struct {
	History      []E
	Instructions *domain.ProjectInstructionsSnapshot
	WorkMode     domain.WorkMode
	Tools        tool.FacadeView
	Inference    domain.InferenceSettings
	Skills       domain.SkillCatalog
}

// ReadSamplingSources 按现有顺序验证输入并选取原生窗口、项目指令和工具视图。
func ReadSamplingSources[E any](input TurnInput, family domain.ProviderFamily, activeWindow func() []E) (SamplingSources[E], error) {
	if err := input.Validate(); err != nil {
		return SamplingSources[E]{}, err
	}
	inference := input.InferenceSettings()
	if inference.Family() != family {
		return SamplingSources[E]{}, fmt.Errorf("sampling inference family mismatch")
	}
	history := activeWindow()
	snapshot, exists, err := input.ProjectInstructions()
	if err != nil {
		return SamplingSources[E]{}, err
	}
	var instructions *domain.ProjectInstructionsSnapshot
	if exists {
		instructions = &snapshot
	}
	catalog, exists, err := input.ToolCatalog()
	if err != nil || !exists {
		return SamplingSources[E]{}, fmt.Errorf("tool catalog is required")
	}
	view, err := catalog.View(family)
	if err != nil {
		return SamplingSources[E]{}, err
	}
	return SamplingSources[E]{History: history, Instructions: instructions, WorkMode: input.WorkMode(), Tools: view, Inference: inference, Skills: input.SkillCatalog()}, nil
}

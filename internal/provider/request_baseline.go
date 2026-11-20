package provider

import (
	"fmt"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// RequestBaseline 保存一个已提交请求的原生前缀证据，不含连接所有权。
type RequestBaseline struct {
	items      []codec.CanonicalJSON
	properties codec.CanonicalJSON
	present    bool
}

// NewRequestBaseline 在纯内存中冻结请求输入和完成输出。
func NewRequestBaseline(input, output []codec.CanonicalJSON, properties codec.CanonicalJSON) (RequestBaseline, error) {
	if err := properties.Validate(MaxNativeCommitBytes); err != nil {
		return RequestBaseline{}, err
	}
	items := make([]codec.CanonicalJSON, 0, len(input)+len(output))
	size := 0
	for _, parts := range [][]codec.CanonicalJSON{input, output} {
		for _, item := range parts {
			if err := item.Validate(MaxNativeCommitBytes); err != nil {
				return RequestBaseline{}, err
			}
			size += item.Len()
			if size > 2*MaxNativeCommitBytes {
				return RequestBaseline{}, fmt.Errorf("request baseline exceeds size limit")
			}
			items = append(items, item)
		}
	}
	return RequestBaseline{items: items, properties: properties, present: true}, nil
}

// Compare 校验完整字节前缀并返回后缀，不执行语义或 opaque 字段裁剪。
func (b RequestBaseline) Compare(input []codec.CanonicalJSON, properties codec.CanonicalJSON) RequestComparison {
	if !b.present {
		return RequestComparison{relation: domain.NativeFirst, reason: domain.ResetFirst}
	}
	if !b.properties.Equal(properties) {
		return RequestComparison{relation: domain.NativeReplaced, reason: domain.ResetProperties}
	}
	if len(input) < len(b.items) {
		return RequestComparison{relation: domain.NativeReplaced, reason: domain.ResetPrefix}
	}
	for i, item := range b.items {
		if !item.Equal(input[i]) {
			return RequestComparison{relation: domain.NativeReplaced, reason: domain.ResetPrefix}
		}
	}
	return RequestComparison{offset: len(b.items), relation: domain.NativeAppend, reason: domain.ResetNone}
}

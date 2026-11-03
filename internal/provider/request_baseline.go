package provider

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"fmt"
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
			items = append(items, item.Clone())
		}
	}
	return RequestBaseline{items: items, properties: properties.Clone(), present: true}, nil
}

// Compare 校验完整字节前缀并返回后缀，不执行语义或 opaque 字段裁剪。
func (b RequestBaseline) Compare(input []codec.CanonicalJSON, properties codec.CanonicalJSON) (int, domain.NativeRelation, domain.RequestResetReason) {
	if !b.present {
		return 0, domain.NativeFirst, domain.ResetFirst
	}
	if !bytes.Equal(b.properties.Bytes(), properties.Bytes()) {
		return 0, domain.NativeReplaced, domain.ResetProperties
	}
	if len(input) < len(b.items) {
		return 0, domain.NativeReplaced, domain.ResetPrefix
	}
	for i, item := range b.items {
		if !bytes.Equal(item.Bytes(), input[i].Bytes()) {
			return 0, domain.NativeReplaced, domain.ResetPrefix
		}
	}
	return len(b.items), domain.NativeAppend, domain.ResetNone
}

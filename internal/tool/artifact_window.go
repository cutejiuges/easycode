package tool

import "errors"

// ArtifactWindow 是完整性验证后的有界原始窗口，由 turn owner 按顺序分配额度后渲染。
// 字节仅用于该范围，既不构成文件观察，也不能转成新的保存对象。
type ArtifactWindow struct {
	descriptor ArtifactDescriptor
	input      ReadArtifactInput
	offset     uint64
	data       []byte
}

// NewArtifactWindow 复制已验证窗口，禁止无界正文进入执行材料。
func NewArtifactWindow(descriptor ArtifactDescriptor, input ReadArtifactInput, offset uint64, data []byte) (ArtifactWindow, error) {
	v := ArtifactWindow{descriptor, input, offset, nil}
	if descriptor.Validate() != nil || input.Validate() != nil || descriptor.ID() != input.ID() || input.Offset() > uint64(descriptor.SavedBytes()) {
		return ArtifactWindow{}, ArtifactReadInvalidRange
	}
	start := input.Offset() - min(input.Offset(), 3)
	end := min(uint64(descriptor.SavedBytes()), input.Offset()+input.Limit()+3)
	if offset != start || uint64(len(data)) != end-start || len(data) > ArtifactPageBytes+6 {
		return ArtifactWindow{}, errors.New("invalid artifact window")
	}
	v.data = append([]byte(nil), data...)
	return v, nil
}

// Render 仅在内存中按最终额度生成页，页头与转义一起计费。
func (v ArtifactWindow) Render(allowance int) (ArtifactPage, error) {
	return RenderArtifactRange(v.descriptor, v.input, v.offset, v.data, allowance)
}

// RenderBefore 在已验证窗口内寻找字符边界，返回紧邻 end 的前一页。
// 调用者须为最坏四倍转义预留额度；失败时不返回跳字节的成功页。
func (v ArtifactWindow) RenderBefore(end uint64, allowance int) (ArtifactPage, error) {
	if end <= v.input.Offset() || end > v.input.Offset()+v.input.Limit() {
		return ArtifactPage{}, ArtifactReadInvalidRange
	}
	for shift := uint64(0); shift < 4 && v.input.Offset()+shift < end; shift++ {
		input, err := NewReadArtifactInput(v.input.ID(), v.input.Offset()+shift, end-v.input.Offset()-shift)
		if err != nil {
			return ArtifactPage{}, err
		}
		page, err := RenderArtifactRange(v.descriptor, input, v.offset, v.data, allowance)
		if err == nil && page.NextOffset() == end {
			return page, nil
		}
	}
	return ArtifactPage{}, ArtifactReadInvalidRange
}

package domain

import "errors"

// ReasoningBinding 将活动块关联到同一 sample 的原生位置，不持久化。
type ReasoningBinding struct {
	Block        uint32                `json:"block"`
	Item         uint32                `json:"item"`
	Source       ReasoningSource       `json:"source"`
	Availability ReasoningAvailability `json:"availability"`
}

// NewReasoningBinding 创建一个没有正文或 Provider ID 的当前绑定。
func NewReasoningBinding(block, item uint32, source ReasoningSource, availability ReasoningAvailability) (ReasoningBinding, error) {
	v := ReasoningBinding{Block: block, Item: item, Source: source, Availability: availability}
	return v, v.Validate()
}

// Validate 检查单项身份和允许来源。
func (v ReasoningBinding) Validate() error {
	if v.Block >= MaxReasoningNodes || !v.Source.Valid() || !v.Availability.Valid() {
		return errors.New("invalid reasoning binding")
	}
	return nil
}

// SamplePresentation 是已校验、不可变的原生位置附属值，不进入模型请求。
type SamplePresentation struct {
	items    uint32
	bindings []ReasoningBinding
}

// EmptySamplePresentation 明确创建没有推理的当前 sample 附属值。
func EmptySamplePresentation(items uint32) SamplePresentation {
	return SamplePresentation{items: items, bindings: []ReasoningBinding{}}
}

// NewSamplePresentation 在复制前校验数量、唯一性及原生位置边界。
func NewSamplePresentation(items uint32, bindings []ReasoningBinding) (SamplePresentation, error) {
	if len(bindings) > MaxReasoningNodes {
		return SamplePresentation{}, errors.New("reasoning bindings exceed count limit")
	}
	blocks := make(map[uint32]bool, len(bindings))
	positions := make(map[uint32]bool, len(bindings))
	for _, binding := range bindings {
		if binding.Validate() != nil || binding.Item >= items || blocks[binding.Block] || positions[binding.Item] {
			return SamplePresentation{}, errors.New("invalid sample presentation binding")
		}
		blocks[binding.Block], positions[binding.Item] = true, true
	}
	for block := range len(bindings) {
		if !blocks[uint32(block)] {
			return SamplePresentation{}, errors.New("sample presentation has missing blocks")
		}
	}
	return SamplePresentation{items: items, bindings: append([]ReasoningBinding{}, bindings...)}, nil
}

// ItemCount 返回该 sample 完整原生 item 数量。
func (p SamplePresentation) ItemCount() uint32 { return p.items }

// Bindings 返回不共享可变集合的绑定表。
func (p SamplePresentation) Bindings() []ReasoningBinding {
	return append([]ReasoningBinding{}, p.bindings...)
}

// Validate 拒绝未构造或损坏的当前附属值。
func (p SamplePresentation) Validate() error {
	if p.bindings == nil {
		return errors.New("sample presentation is required")
	}
	_, err := NewSamplePresentation(p.items, p.bindings)
	return err
}

// Clone 保持纯内存复制，不转移 prepared 生命周期。
func (p SamplePresentation) Clone() (SamplePresentation, error) {
	if p.bindings == nil {
		return SamplePresentation{}, errors.New("sample presentation is required")
	}
	return NewSamplePresentation(p.items, p.bindings)
}

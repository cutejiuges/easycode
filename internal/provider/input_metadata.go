package provider

import (
	"easycode/internal/domain"
	"fmt"
	"slices"
)

// InputMetadata 是两家原生存储共用的关联元数据，不属于模型 wire。
type InputMetadata struct {
	TurnID      domain.TurnID         `json:"turn_id"`
	Placement   domain.InputPlacement `json:"placement"`
	SampleIndex uint32                `json:"target_sample_index"`
	InputIDs    []domain.InputID      `json:"input_ids"`
}

// NewInputMetadata 将已校验关联复制为纯存储字段。
func NewInputMetadata(a domain.InputAssociation) (InputMetadata, error) {
	if err := a.Validate(); err != nil {
		return InputMetadata{}, err
	}
	return InputMetadata{TurnID: a.TurnID(), Placement: a.Placement(), SampleIndex: a.SampleIndex(), InputIDs: a.IDs()}, nil
}

// Association 严格校验并返回独立领域关联。
func (m InputMetadata) Association() (domain.InputAssociation, error) {
	return domain.NewInputAssociation(m.TurnID, m.Placement, m.SampleIndex, m.InputIDs)
}

// Matches 校验原生元数据与 Session 回放关联一致。
func (m InputMetadata) Matches(a domain.InputAssociation) bool {
	_, err := m.Association()
	return err == nil && a.Validate() == nil && m.TurnID == a.TurnID() && m.Placement == a.Placement() && m.SampleIndex == a.SampleIndex() && slices.Equal(m.InputIDs, a.IDs())
}

// ValidateUserInputs 检查关联顺序、身份和正文完全一致。
func ValidateUserInputs(a domain.InputAssociation, inputs []domain.UserInput) error {
	if err := a.Validate(); err != nil {
		return err
	}
	ids := a.IDs()
	if len(ids) != len(inputs) {
		return fmt.Errorf("input count does not match association")
	}
	for i, input := range inputs {
		if input.Validate() != nil || input.ID() != ids[i] {
			return fmt.Errorf("input does not match association")
		}
	}
	return nil
}

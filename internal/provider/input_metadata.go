package provider

import (
	"easycode/internal/domain"
	"fmt"
	"slices"
)

// InputMetadata 是两家原生存储共用的关联元数据，不属于模型 wire。
type InputMetadata struct {
	Skills      []InputSkillMetadata  `json:"skills,omitempty"`
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
	seen := make(map[domain.InputID]bool)
	for _, v := range m.Skills {
		if !slices.Contains(m.InputIDs, v.InputID) || seen[v.InputID] || v.Attachment.Validate() != nil {
			return domain.InputAssociation{}, fmt.Errorf("invalid input skill metadata")
		}
		seen[v.InputID] = true
	}
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

// InputSkillMetadata 关联原生用户消息中的独立技能文本块。
type InputSkillMetadata struct {
	InputID    domain.InputID         `json:"input_id"`
	Attachment domain.SkillAttachment `json:"attachment"`
}

// Skill 返回对应输入的冻结附件副本。
func (m InputMetadata) Skill(id domain.InputID) (domain.SkillAttachment, bool) {
	for _, v := range m.Skills {
		if v.InputID == id {
			return v.Attachment, true
		}
	}
	return domain.SkillAttachment{}, false
}

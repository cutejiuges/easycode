package provider

import (
	"easycode/internal/codec"
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
	Files       []InputFileMetadata   `json:"files"`
}

// NewInputMetadata 将已校验关联复制为纯存储字段。
func NewInputMetadata(a domain.InputAssociation) (InputMetadata, error) {
	if err := a.Validate(); err != nil {
		return InputMetadata{}, err
	}
	return InputMetadata{TurnID: a.TurnID(), Placement: a.Placement(), SampleIndex: a.SampleIndex(), InputIDs: a.IDs(), Files: make([]InputFileMetadata, 0)}, nil
}

// Association 严格校验并返回独立领域关联。
func (m InputMetadata) Association() (domain.InputAssociation, error) {
	if m.Files == nil {
		return domain.InputAssociation{}, fmt.Errorf("input files metadata is required")
	}
	previous := -1
	for _, group := range m.Files {
		index := slices.Index(m.InputIDs, group.InputID)
		if index <= previous || group.Attachments.Len() == 0 || group.Attachments.Validate() != nil {
			return domain.InputAssociation{}, fmt.Errorf("invalid input file metadata")
		}
		previous = index
	}
	seen := make(map[domain.InputID]bool)
	for _, v := range m.Skills {
		if !slices.Contains(m.InputIDs, v.InputID) || seen[v.InputID] || v.Attachment.Validate() != nil {
			return domain.InputAssociation{}, fmt.Errorf("invalid input skill metadata")
		}
		seen[v.InputID] = true
	}
	return domain.NewInputAssociation(m.TurnID, m.Placement, m.SampleIndex, m.InputIDs)
}

// InputFileMetadata 关联同一输入的完整有序文件集合，不保存推理策略。
type InputFileMetadata struct {
	InputID     domain.InputID         `json:"input_id"`
	Attachments domain.FileAttachments `json:"attachments"`
}

// NewInputFileMetadata 创建非空的输入关联，并复制文件集合。
func NewInputFileMetadata(id domain.InputID, attachments domain.FileAttachments) (InputFileMetadata, error) {
	v := InputFileMetadata{InputID: id, Attachments: attachments.Clone()}
	return v, v.Validate()
}
func (v InputFileMetadata) Validate() error {
	if !v.InputID.Valid() || v.Attachments.Len() == 0 || v.Attachments.Validate() != nil {
		return fmt.Errorf("invalid input file metadata")
	}
	return nil
}

// DecodeInputFileMetadata 严格校验关联项的全部当前字段。
func DecodeInputFileMetadata(data []byte) (InputFileMetadata, error) {
	type fields InputFileMetadata
	var value fields
	if len(data) > domain.MaxFileAttachmentsBytes+4096 || codec.DecodeObject(data, &value, "input_id", "attachments") != nil {
		return InputFileMetadata{}, fmt.Errorf("invalid input file metadata encoding")
	}
	v := InputFileMetadata(value)
	return v, v.Validate()
}
func (v *InputFileMetadata) UnmarshalJSON(data []byte) error {
	value, err := DecodeInputFileMetadata(data)
	if err == nil {
		*v = value
	}
	return err
}

// DecodeInputMetadata 只接受必需 files 数组的唯一当前结构，不修复或选择历史 reader。
func DecodeInputMetadata(data []byte) (InputMetadata, error) {
	type fields InputMetadata
	var value fields
	if len(data) > MaxNativeCommitBytes || codec.DecodeObject(data, &value, "turn_id", "placement", "target_sample_index", "input_ids", "files") != nil {
		return InputMetadata{}, fmt.Errorf("invalid input metadata encoding")
	}
	m := InputMetadata(value)
	_, err := m.Association()
	return m, err
}
func (m *InputMetadata) UnmarshalJSON(data []byte) error {
	value, err := DecodeInputMetadata(data)
	if err == nil {
		*m = value
	}
	return err
}

// FileAttachments 返回独立集合，缺少关联表示该输入没有文件。
func (m InputMetadata) FileAttachments(id domain.InputID) domain.FileAttachments {
	for _, group := range m.Files {
		if group.InputID == id {
			return group.Attachments.Clone()
		}
	}
	return domain.FileAttachments{}
}

// Clone 复制关联集合并保留非法 nil 状态，使校验不能静默修复旧结构。
func (m InputMetadata) Clone() InputMetadata {
	m.InputIDs = slices.Clone(m.InputIDs)
	m.Skills = slices.Clone(m.Skills)
	if m.Files != nil {
		files := make([]InputFileMetadata, len(m.Files))
		for i, group := range m.Files {
			files[i] = InputFileMetadata{group.InputID, group.Attachments.Clone()}
		}
		m.Files = files
	}
	return m
}

// TextBlocks 校验原文关联后派生预期块，仅供核对，不替换已存的 Provider-native 文本。
func (m InputMetadata) TextBlocks(id domain.InputID, text string) ([]string, error) {
	if m.Files == nil || !slices.Contains(m.InputIDs, id) {
		return nil, fmt.Errorf("invalid input metadata association")
	}
	files := m.FileAttachments(id)
	input, err := domain.NewUserInput(id, text)
	if err != nil {
		return nil, err
	}
	blocks := []string{text}
	if skill, ok := m.Skill(id); ok {
		input, err = input.WithSkillAttachment(skill)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, skill.Content)
	}
	if _, err = input.WithFileAttachments(files); err != nil {
		return nil, err
	}
	for _, attachment := range files.Items() {
		rendered, err := attachment.RenderedText()
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, rendered)
	}
	return blocks, nil
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

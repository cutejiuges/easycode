package session

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
	"fmt"
)

// SkillInputPayload 保存唯一当前的技能读取参数。
type SkillInputPayload struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Resource  string `json:"resource"`
}

// SkillResultPayload 保存已冻结的技能正文，恢复时不再读盘。
type SkillResultPayload struct {
	Input      SkillInputPayload       `json:"input"`
	Attachment *domain.SkillAttachment `json:"attachment,omitempty"`
}

func encodeSkillInput(i tool.SkillInput) *SkillInputPayload {
	return &SkillInputPayload{i.Name(), i.Arguments(), i.Resource()}
}

// Domain 严格重建主文档或资源读取输入。
func (p SkillInputPayload) Domain() (tool.SkillInput, error) {
	if p.Resource != "" {
		if p.Arguments != "" {
			return tool.SkillInput{}, fmt.Errorf("skill arguments and resource are exclusive")
		}
		return tool.NewSkillResourceInput(p.Name, p.Resource)
	}
	return tool.NewSkillLoadInput(p.Name, p.Arguments)
}

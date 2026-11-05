package builtin

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

type catalogSkillExecutor struct{}

func (catalogSkillExecutor) Execute(context.Context, tool.SkillInvocation, []domain.SkillAttachment) tool.InvocationResult {
	return tool.InvocationResult{}
}

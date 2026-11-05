package tool

import (
	"context"
	"easycode/internal/domain"
)

type catalogSkillExecutor struct{}

func (catalogSkillExecutor) Execute(context.Context, SkillInvocation, []domain.SkillAttachment) InvocationResult {
	return InvocationResult{}
}

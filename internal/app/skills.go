package app

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension/skill"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/tool"
)

// skillExecutor 将来源读取适配为冻结结果，错误不包含底层路径或正文。
type skillExecutor struct{ source *skill.Local }

func (e skillExecutor) Execute(ctx context.Context, v tool.SkillInvocation, selections []domain.SkillAttachment) tool.InvocationResult {
	i := v.Input()
	a, err := e.source.Read(ctx, i.Name(), i.Arguments(), i.Resource(), selections)
	status, code, text := tool.ResultSuccess, "ok", a.Content
	if err != nil {
		status, code = tool.ResultError, skill.ErrorCode(err)
		if ctx.Err() != nil {
			status, code = tool.ResultCancelled, "cancelled"
		}
		text = "Skill failed: " + code
		a = domain.SkillAttachment{}
	}
	p, _ := tool.NewModelPreview(text)
	r, _ := tool.NewSkillInvocationResult(v, status, code, p, a)
	return r
}

// preparedControl 保持 stream reader 的串行所有权，在纯 admission 之前读取附件。
type preparedControl struct {
	*chatRuntime.AgentLoop
	runtime *chatRuntime.Runtime
}

func (p preparedControl) Submit(ctx context.Context, c protocol.Command) (protocol.CommandResult, error) {
	c, err := p.runtime.PrepareInput(ctx, c)
	if err != nil {
		return protocol.CommandResult{}, err
	}
	return p.AgentLoop.Submit(ctx, c)
}

// skillInputPreparer 只把来源安全分类投影为宿主可显示错误。
type skillInputPreparer struct{ source *skill.Local }

func (p skillInputPreparer) Prepare(ctx context.Context, text string) (domain.SkillAttachment, bool, error) {
	a, ok, err := p.source.Prepare(ctx, text)
	if err == nil {
		return a, ok, nil
	}
	if ctx.Err() != nil {
		return domain.SkillAttachment{}, false, ctx.Err()
	}
	return domain.SkillAttachment{}, false, fault.New(fault.CodeInvalidInput, skill.ErrorCode(err))
}

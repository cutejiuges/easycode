package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

// SkillPreparer 是宿主准备边界，队列接收与恢复不调用此接口。
type SkillPreparer interface {
	Prepare(context.Context, string) (domain.SkillAttachment, bool, error)
}

// PrepareInput 在宿主 owner 上冻结显式附件；构造和 admission 保持纯内存。
func (runtime *Runtime) PrepareInput(ctx context.Context, c protocol.Command) (protocol.Command, error) {
	if runtime.skillPreparer == nil || (c.Kind() != protocol.CommandSubmitInput && c.Kind() != protocol.CommandSubmitConversationalInput) {
		return c, nil
	}
	a, ok, err := runtime.skillPreparer.Prepare(ctx, c.Text())
	if err != nil {
		return protocol.Command{}, err
	}
	if ok {
		return c.WithSkillAttachment(a)
	}
	return c, nil
}

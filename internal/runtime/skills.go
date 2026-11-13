package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"sync"
)

// SkillPreparer 是宿主准备边界，队列接收与恢复不调用此接口。
type SkillPreparer interface {
	Prepare(context.Context, string) (domain.SkillAttachment, bool, error)
}

type skillInstallation struct {
	mu                 sync.Mutex
	installed, started bool
}

// InstallSkills 在旧事实 reconciliation 后、任何未来工作开始前恰好安装一次。
func (runtime *Runtime) InstallSkills(catalog domain.SkillCatalog, preparer SkillPreparer) error {
	if runtime == nil || catalog.Validate() != nil || preparer == nil {
		return fault.New(fault.CodeInvalidConfiguration, "invalid skill installation")
	}
	copy := catalog.Clone()
	poisoned := runtime.journal.Poisoned()
	runtime.skillInstallation.mu.Lock()
	defer runtime.skillInstallation.mu.Unlock()
	if runtime.skillInstallation.installed || runtime.skillInstallation.started || runtime.active.Load() || runtime.poisoned.Load() || poisoned {
		return fault.New(fault.CodeInvalidConfiguration, "skill installation is closed")
	}
	runtime.skillCatalog, runtime.skillPreparer = copy, preparer
	runtime.skillInstallation.installed = true
	return nil
}
func (runtime *Runtime) markSkillsStarted() {
	runtime.skillInstallation.mu.Lock()
	runtime.skillInstallation.started = true
	runtime.skillInstallation.mu.Unlock()
}

// PrepareInput 在宿主 owner 上冻结显式附件；构造和 admission 保持纯内存。
func (runtime *Runtime) PrepareInput(ctx context.Context, c protocol.Command) (protocol.Command, error) {
	runtime.markSkillsStarted()
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

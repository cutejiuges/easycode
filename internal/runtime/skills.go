package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"sync"
)

// InputPreparer 是实际宿主的附件准备边界，队列接收与恢复不调用此接口。
type InputPreparer interface {
	Prepare(context.Context, protocol.Command) (protocol.Command, error)
}

type skillInstallation struct {
	mu                 sync.Mutex
	installed, started bool
}

// InstallInputSources 在旧事实 reconciliation 后、任何未来工作开始前恰好安装目录和准备策略。
func (runtime *Runtime) InstallInputSources(catalog domain.SkillCatalog, preparer InputPreparer) error {
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
	runtime.skillCatalog, runtime.inputPreparer = copy, preparer
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
	if c.Kind() != protocol.CommandSubmitInput && c.Kind() != protocol.CommandSubmitConversationalInput {
		return c, nil
	}
	if err := c.Validate(); err != nil {
		return protocol.Command{}, err
	}
	if runtime.inputPreparer == nil {
		if c.InputDraft().SkillIntent() == domain.InputSkillRequired {
			return protocol.Command{}, fault.New(fault.CodeInvalidInput, "skill_unavailable")
		}
		if c.ValidateForAdmission() != nil {
			return protocol.Command{}, fault.New(fault.CodeInvalidInput, "file_reference_unavailable")
		}
		return c, nil
	}
	prepared, err := runtime.inputPreparer.Prepare(ctx, c)
	if err != nil {
		return protocol.Command{}, err
	}
	if prepared.Kind() != c.Kind() || prepared.RequestID() != c.RequestID() || prepared.Text() != c.Text() ||
		prepared.InputDraft().SkillIntent() != c.InputDraft().SkillIntent() || prepared.ValidateForAdmission() != nil {
		return protocol.Command{}, fault.New(fault.CodeInvalidInput, "invalid prepared input")
	}
	return prepared, nil
}

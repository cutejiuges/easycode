package builtin

import (
	"context"
	"errors"
	"os"

	"easycode/internal/tool"
)

// patchFile 绑定已验证的父目录、实际目标句柄与唯一清理owner。
type patchFile interface {
	file() *os.File
	evidence([]byte) (tool.FileBaseEvidence, error)
	replace(context.Context, tool.FileBaseEvidence, []byte, patchHooks) (bool, error)
	close() error
}

// patchHooks 提供确定性故障边界，默认操作不依赖可变全局状态。
type patchHooks struct {
	write         func(*os.File, []byte) error
	fileSync      func(*os.File) error
	beforeRename  func()
	rename        func(int, string, string) error
	directorySync func(*os.File) error
	cleanup       func(int, string) error
}

var errPatchConflict = errors.New("patch base conflicts with candidate")

// PatchPreparer 只读取已打开Workspace中的目标，不创建或修改文件。
type PatchPreparer struct{ workspace *Workspace }

// NewPatchPreparer 纯内存组合候选准备器。
func NewPatchPreparer(workspace *Workspace) (*PatchPreparer, error) {
	if workspace == nil || workspace.handle == nil {
		return nil, errors.New("workspace is required")
	}
	return &PatchPreparer{workspace: workspace}, nil
}

// Prepare 读取安全句柄并生成绑定最终bytes的完整候选。
func (preparer *PatchPreparer) Prepare(ctx context.Context, invocation tool.PatchInvocation) (tool.PatchCandidate, error) {
	if ctx == nil || ctx.Err() != nil {
		return tool.PatchCandidate{}, patchError("cancelled")
	}
	if preparer == nil || preparer.workspace == nil || invocation.Validate() != nil {
		return tool.PatchCandidate{}, patchError("invalid_invocation")
	}
	target, err := preparer.workspace.handle.openPatchFile(invocation.Input().FilePath(), preparer.workspace.hooks)
	if err != nil {
		return tool.PatchCandidate{}, safePatchOpenError(err)
	}
	defer func() { _ = target.close() }()
	base, code, _ := readBounded(ctx, target.file())
	if code != "" {
		return tool.PatchCandidate{}, patchError(code)
	}
	evidence, err := target.evidence(base)
	if err != nil {
		return tool.PatchCandidate{}, safePatchOpenError(err)
	}
	final, err := applyPatchPlan(base, invocation.Input())
	if err != nil {
		return tool.PatchCandidate{}, err
	}
	diff, summary, err := buildPatchDiff(invocation.Input().FilePath(), base, final, len(invocation.Input().Replacements()))
	if err != nil {
		return tool.PatchCandidate{}, err
	}
	fingerprint, err := tool.PatchFingerprint(invocation.Input(), evidence, final)
	if err != nil {
		return tool.PatchCandidate{}, patchError("invalid_candidate")
	}
	if ctx.Err() != nil {
		return tool.PatchCandidate{}, patchError("cancelled")
	}
	return tool.NewPatchCandidate(invocation, evidence, final, diff, fingerprint, summary)
}

// PatchExecutor 在Runtime完成durable批准与started后执行一个候选。
type PatchExecutor struct {
	workspace *Workspace
	hooks     patchHooks
}

// NewPatchExecutor 纯内存组合安全执行器。
func NewPatchExecutor(workspace *Workspace) (*PatchExecutor, error) {
	if workspace == nil || workspace.handle == nil {
		return nil, errors.New("workspace is required")
	}
	return &PatchExecutor{workspace: workspace}, nil
}

// Execute 重新核验基线后原子替换；rename后任何不确定结果均禁止安全重试。
func (executor *PatchExecutor) Execute(ctx context.Context, candidate tool.PatchCandidate) tool.InvocationResult {
	call, _ := tool.NewPatchReadyCall(candidate.ProviderCallID(), candidate.WireKind(), candidate.Input())
	invocation, _ := tool.NewPatchInvocation(candidate.InvocationID(), call)
	failed := func(status tool.ResultStatus, code string) tool.InvocationResult {
		return tool.NewPatchErrorResult(invocation, status, code, "Patch failed: "+code)
	}
	if ctx == nil || ctx.Err() != nil {
		return failed(tool.ResultCancelled, "cancelled")
	}
	if executor == nil || executor.workspace == nil || candidate.Validate() != nil {
		return failed(tool.ResultError, "invalid_candidate")
	}
	target, err := executor.workspace.handle.openPatchFile(candidate.RelativePath(), executor.workspace.hooks)
	if err != nil {
		return failed(tool.ResultConflict, "patch_conflict")
	}
	defer func() { _ = target.close() }()
	base, code, _ := readBounded(ctx, target.file())
	if code != "" {
		if ctx.Err() != nil {
			return failed(tool.ResultCancelled, "cancelled")
		}
		return failed(tool.ResultConflict, "patch_conflict")
	}
	evidence, err := target.evidence(base)
	if err != nil || evidence != candidate.Evidence() {
		return failed(tool.ResultConflict, "patch_conflict")
	}
	committed, err := target.replace(ctx, candidate.Evidence(), candidate.FinalBytes(), executor.hooks)
	if err != nil {
		if committed {
			return failed(tool.ResultOutcomeUncertain, "patch_durability_uncertain")
		}
		if errors.Is(err, errPatchConflict) {
			return failed(tool.ResultConflict, "patch_conflict")
		}
		if ctx.Err() != nil {
			return failed(tool.ResultCancelled, "cancelled")
		}
		return failed(tool.ResultError, "patch_write_failed")
	}
	return tool.NewPatchSuccessResult(candidate)
}

func safePatchOpenError(err error) error {
	switch {
	case errors.Is(err, errWorkspaceUnsupported):
		return patchError("workspace_unsupported")
	case errors.Is(err, errTargetNotRegular):
		return patchError("not_regular_file")
	case errors.Is(err, errTargetNotReadable):
		return patchError("permission_denied")
	case errors.Is(err, errTargetTooLarge):
		return patchError("file_too_large")
	default:
		return patchError("patch_target_unavailable")
	}
}

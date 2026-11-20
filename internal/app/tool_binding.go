package app

import (
	"easycode/internal/fault"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
)

// workspaceTools 组装目录和 Runtime 共同使用的工作区工具与审批策略。
type workspaceTools struct {
	read         tool.ReadExecutor
	glob         tool.GlobExecutor
	grep         tool.GrepExecutor
	patchPrepare tool.PatchPreparer
	patch        tool.PatchExecutor
	writePrepare tool.WritePreparer
	write        tool.WriteExecutor
	execPrepare  tool.ExecPreparer
	policy       tool.Policy
}

func newWorkspaceTools(workspace *builtin.Workspace) (workspaceTools, error) {
	read, err := builtin.NewReadExecutor(workspace)
	if err != nil {
		return workspaceTools{}, fault.New(fault.CodeInvalidConfiguration, "Read executor is unavailable")
	}
	glob, err := builtin.NewGlobExecutor(workspace)
	if err != nil {
		return workspaceTools{}, fault.New(fault.CodeInvalidConfiguration, "Glob executor is unavailable")
	}
	grep, err := builtin.NewGrepExecutor(workspace)
	if err != nil {
		return workspaceTools{}, fault.New(fault.CodeInvalidConfiguration, "Grep executor is unavailable")
	}
	patchPrepare, err := builtin.NewPatchPreparer(workspace)
	if err != nil {
		return workspaceTools{}, fault.New(fault.CodeInvalidConfiguration, "Patch preparer is unavailable")
	}
	patch, err := builtin.NewPatchExecutor(workspace)
	if err != nil {
		return workspaceTools{}, fault.New(fault.CodeInvalidConfiguration, "Patch executor is unavailable")
	}
	writePrepare, err := builtin.NewWritePreparer(workspace)
	if err != nil {
		return workspaceTools{}, fault.New(fault.CodeInvalidConfiguration, "Write preparer is unavailable")
	}
	write, err := builtin.NewWriteExecutor(workspace)
	if err != nil {
		return workspaceTools{}, fault.New(fault.CodeInvalidConfiguration, "Write executor is unavailable")
	}
	return workspaceTools{
		read: read, glob: glob, grep: grep, patchPrepare: patchPrepare, patch: patch,
		writePrepare: writePrepare, write: write, policy: tool.NewApprovalPolicy(), execPrepare: builtin.NewExecPlanner(),
	}, nil
}

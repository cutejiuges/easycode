// Package testsupport 只供 Provider 契约测试共享装配，正式程序不得依赖本包。
package testsupport

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

type inertReadExecutor struct{}

func (inertReadExecutor) Execute(context.Context, tool.ReadInvocation) tool.ReadExecution {
	return tool.ReadExecution{}
}

type inertGlobExecutor struct{}

func (inertGlobExecutor) Execute(context.Context, tool.GlobInvocation, tool.TextOutputCapture) tool.SearchExecution {
	return tool.SearchExecution{}
}

type inertGrepExecutor struct{}

func (inertGrepExecutor) Execute(context.Context, tool.GrepInvocation, tool.TextOutputCapture) tool.SearchExecution {
	return tool.SearchExecution{}
}

type inertPatchPreparer struct{}

func (inertPatchPreparer) Prepare(context.Context, tool.PatchInvocation) (tool.PatchCandidate, error) {
	return tool.PatchCandidate{}, nil
}

type inertPatchExecutor struct{}

func (inertPatchExecutor) Execute(context.Context, tool.PatchCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

func ToolCatalog(t *testing.T) tool.CatalogSnapshot {
	t.Helper()
	binding, err := tool.NewBinding(tool.Components{
		Read:         inertReadExecutor{},
		Glob:         inertGlobExecutor{},
		Grep:         inertGrepExecutor{},
		PreparePatch: inertPatchPreparer{},
		Patch:        inertPatchExecutor{},
		PrepareWrite: catalogWritePreparer{},
		Write:        catalogWriteExecutor{},
		PrepareExec:  inertExecComponents{},
		Exec:         inertExecComponents{},
		Policy:       tool.NewApprovalPolicy(),
		ReadArtifact: catalogArtifactExecutor{},
		Skill:        catalogSkillExecutor{},
		Question:     catalogQuestionFixture{},
		Task:         catalogTaskFixture{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return binding.Catalog()
}

type inertExecComponents struct{}

func (inertExecComponents) Prepare(tool.ExecInvocation) (tool.ExecCandidate, error) {
	return tool.ExecCandidate{}, nil
}
func (inertExecComponents) Execute(context.Context, tool.ExecCandidate, func(tool.ExecProgress)) tool.InvocationResult {
	return tool.InvocationResult{}
}

// catalogWritePreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogWritePreparer struct{}

func (catalogWritePreparer) Prepare(context.Context, tool.WriteInvocation, tool.ReadObservation) (tool.WriteCandidate, error) {
	return tool.WriteCandidate{}, nil
}

type catalogWriteExecutor struct{}

func (catalogWriteExecutor) Execute(context.Context, tool.WriteCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

type catalogArtifactExecutor struct{}

func (catalogArtifactExecutor) Execute(_ context.Context, v tool.ReadArtifactInvocation) tool.ReadArtifactExecution {
	p, _ := tool.NewModelPreview("ReadArtifact failed: unavailable")
	r, _ := tool.NewReadArtifactInvocationResult(v, tool.ResultError, "unavailable", p, tool.ArtifactPage{})
	execution, _ := tool.NewReadArtifactFailure(r)
	return execution
}

// catalogQuestionFixture 为既有目录测试提供显式组件，不改变其测试目标。
type catalogQuestionFixture struct{}

func (catalogQuestionFixture) Execute(context.Context, tool.QuestionInvocation, tool.QuestionResponder) tool.InvocationResult {
	return tool.InvocationResult{}
}

// catalogTaskFixture 为目录提供真实的纯内存任务消费者。
type catalogTaskFixture struct{}

func (catalogTaskFixture) Execute(_ context.Context, invocation tool.TaskInvocation) tool.InvocationResult {
	result, _ := tool.NewTaskInvocationResult(invocation)
	return result
}

type catalogSkillExecutor struct{}

func (catalogSkillExecutor) Execute(context.Context, tool.SkillInvocation, []domain.SkillAttachment) tool.InvocationResult {
	return tool.InvocationResult{}
}

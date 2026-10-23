package openai

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

type inertReadExecutor struct{}

func (inertReadExecutor) Execute(context.Context, tool.ReadInvocation) tool.ReadExecution {
	return tool.ReadExecution{}
}

type inertGlobExecutor struct{}

func (inertGlobExecutor) Execute(context.Context, tool.GlobInvocation) tool.InvocationResult {
	return tool.InvocationResult{}
}

type inertGrepExecutor struct{}

func (inertGrepExecutor) Execute(context.Context, tool.GrepInvocation) tool.InvocationResult {
	return tool.InvocationResult{}
}

type inertPatchPreparer struct{}

func (inertPatchPreparer) Prepare(context.Context, tool.PatchInvocation) (tool.PatchCandidate, error) {
	return tool.PatchCandidate{}, nil
}

type inertPatchExecutor struct{}

func (inertPatchExecutor) Execute(context.Context, tool.PatchCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

func testOpenAIToolCatalog(t *testing.T) tool.CatalogSnapshot {
	t.Helper()
	catalog, err := tool.NewCatalogSnapshot(inertReadExecutor{}, inertGlobExecutor{}, inertGrepExecutor{}, inertPatchPreparer{}, inertPatchExecutor{}, catalogWritePreparer{}, catalogWriteExecutor{}, tool.NewApprovalPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func testOpenAIToolView(t *testing.T) tool.FacadeView {
	t.Helper()
	view, err := testOpenAIToolCatalog(t).View(domain.ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func testOpenAITurnInput(t *testing.T, text string) provider.TurnInput {
	t.Helper()
	input, err := (provider.TurnInput{Text: text}).WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func nativeItemPointer(item NativeItem) *NativeItem {
	owned := item.clone()
	return &owned
}

func testSampleEntry(user NativeItem, outputs []NativeItem) nativeHistoryEntry {
	return nativeHistoryEntry{
		Kind: nativeHistorySample, Input: nativeItemPointer(user), Outputs: cloneNativeItems(outputs), Usage: rawUsage{},
	}
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

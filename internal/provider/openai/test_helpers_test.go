package openai

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

type inertReadExecutor struct{}

func (inertReadExecutor) Execute(context.Context, tool.ReadInvocation) tool.InvocationResult {
	return tool.InvocationResult{}
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
	catalog, err := tool.NewExecCatalogSnapshot(inertReadExecutor{}, inertGlobExecutor{}, inertGrepExecutor{}, inertPatchPreparer{}, inertPatchExecutor{}, inertExecComponents{}, inertExecComponents{}, tool.RenderExecOutput, tool.NewApprovalPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

type inertExecComponents struct{}

func (inertExecComponents) Prepare(tool.ExecInvocation) (tool.ExecCandidate, error) {
	return tool.ExecCandidate{}, nil
}
func (inertExecComponents) Execute(context.Context, tool.ExecCandidate, func(tool.ExecProgress)) tool.InvocationResult {
	return tool.InvocationResult{}
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

package anthropic

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

func testAnthropicToolCatalog(t *testing.T) tool.CatalogSnapshot {
	t.Helper()
	catalog, err := tool.NewPatchCatalogSnapshot(inertReadExecutor{}, inertGlobExecutor{}, inertGrepExecutor{}, inertPatchPreparer{}, inertPatchExecutor{}, tool.NewApprovalPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func testAnthropicToolView(t *testing.T) tool.FacadeView {
	t.Helper()
	view, err := testAnthropicToolCatalog(t).View(domain.ProviderAnthropic)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func testAnthropicTurnInput(t *testing.T, text string) provider.TurnInput {
	t.Helper()
	input, err := (provider.TurnInput{Text: text}).WithToolCatalog(testAnthropicToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func nativeMessagePointer(message nativeMessage) *nativeMessage {
	owned := message.clone()
	return &owned
}

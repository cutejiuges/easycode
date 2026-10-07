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

func testOpenAIToolCatalog(t *testing.T) tool.CatalogSnapshot {
	t.Helper()
	catalog, err := tool.NewReadCatalogSnapshot(inertReadExecutor{})
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

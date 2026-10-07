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

func testAnthropicToolCatalog(t *testing.T) tool.CatalogSnapshot {
	t.Helper()
	catalog, err := tool.NewReadCatalogSnapshot(inertReadExecutor{})
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

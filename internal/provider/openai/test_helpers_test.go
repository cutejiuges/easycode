package openai

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
	"easycode/internal/tool"
)

func testOpenAIToolView(t *testing.T) tool.FacadeView {
	t.Helper()
	view, err := testsupport.ToolCatalog(t).View(domain.ProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func testOpenAITurnInput(t *testing.T, conversation provider.Conversation, text string) provider.TurnInput {
	t.Helper()
	testCommitUserInput(t, conversation.(*Conversation), text)
	input, err := testSamplingInput(t, conversation.(*Conversation).provider.config.Inference).WithToolCatalog(testsupport.ToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func nativeItemPointer(item NativeItem) *NativeItem {
	owned := item.clone()
	return &owned
}

// testConversation 使用当前显式身份契约创建测试会话。
func testConversation(t *testing.T, instance *Provider) *Conversation {
	t.Helper()
	value, err := instance.NewConversation("01900000-0000-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	return value.(*Conversation)
}

func testSamplingInput(t *testing.T, settings domain.InferenceSettings) provider.TurnInput {
	t.Helper()
	input, err := provider.NewSamplingInput(settings)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

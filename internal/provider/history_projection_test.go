package provider_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/anthropic"
	"easycode/internal/provider/openai"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

type inertReadExecutor struct{}

func (inertReadExecutor) Execute(context.Context, tool.ReadInvocation) tool.InvocationResult {
	return tool.InvocationResult{}
}

func TestProviderHistoryProjectorsProduceEquivalentTextSemantics(t *testing.T) {
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"anthropic-private-id\",\"model\":\"claude-test\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\",\"signature\":\"\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"anthropic-private-thinking\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"anthropic-opaque-signature\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"shared answer\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":1}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer anthropicServer.Close()

	openAIServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"response-private-id\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"shared answer\"}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"reasoning\",\"id\":\"openai-private-id\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"openai-private-summary\"}],\"encrypted_content\":\"openai-opaque-encrypted\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"openai-message-id\",\"role\":\"assistant\",\"phase\":\"final\",\"content\":[{\"type\":\"output_text\",\"text\":\"shared answer\"}]}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-private-id\"}}\n\n")
	}))
	defer openAIServer.Close()

	anthropicProvider, err := anthropic.New(anthropic.Config{
		BaseURL: anthropicServer.URL,
		APIKey:  secret.New("anthropic-test-key"),
		Model:   "claude-test",
	})
	if err != nil {
		t.Fatalf("new Anthropic provider: %v", err)
	}
	defer func() {
		if err := anthropicProvider.Close(); err != nil {
			t.Errorf("close Anthropic provider: %v", err)
		}
	}()
	openAIProvider, err := openai.New(openai.Config{
		BaseURL: openAIServer.URL,
		APIKey:  secret.New("openai-test-key"),
		Model:   "gpt-test",
	})
	if err != nil {
		t.Fatalf("new OpenAI provider: %v", err)
	}
	defer func() {
		if err := openAIProvider.Close(); err != nil {
			t.Errorf("close OpenAI provider: %v", err)
		}
	}()

	anthropicView := completeTurnAndProject(t, anthropicProvider.NewConversation(), "shared question")
	openAIView := completeTurnAndProject(t, openAIProvider.NewConversation(), "shared question")
	wantTurns := []domain.SemanticTurn{{UserText: "shared question", AssistantText: "shared answer"}}
	if anthropicView.Provider != domain.ProviderAnthropic || openAIView.Provider != domain.ProviderOpenAI {
		t.Fatalf("provider families: Anthropic=%s OpenAI=%s", anthropicView.Provider, openAIView.Provider)
	}
	if !reflect.DeepEqual(anthropicView.Turns, wantTurns) || !reflect.DeepEqual(openAIView.Turns, wantTurns) {
		t.Fatalf("semantic turns differ: Anthropic=%#v OpenAI=%#v", anthropicView.Turns, openAIView.Turns)
	}
}

func completeTurnAndProject(t *testing.T, conversation provider.Conversation, text string) domain.SemanticHistoryView {
	t.Helper()
	catalog, err := tool.NewReadCatalogSnapshot(inertReadExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	input, err := (provider.TurnInput{Text: text}).WithToolCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := conversation.Stream(context.Background(), input)
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	terminalCount := 0
	for event := range stream {
		if event.Kind().Terminal() {
			terminalCount++
			if event.Kind() != provider.StreamEventCompleted || event.Error() != nil {
				t.Fatalf("turn terminal: %#v", event)
			}
			if event.PreparedSample() == nil {
				t.Fatal("completed terminal is missing prepared sample")
			}
			if err := event.PreparedSample().Finalize(); err != nil {
				t.Fatalf("finalize sample: %v", err)
			}
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal count: %d", terminalCount)
	}
	return conversation.ProjectHistory()
}

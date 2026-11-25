package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func submitAppTurn(t *testing.T, resources *chatResources, text string) {
	t.Helper()
	if resources.session == nil {
		t.Fatal("session missing")
	}
	if err := resources.session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := resources.session.Submit(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	terminalCount := 0
	for event := range events {
		if event.Kind == protocol.EventTurnCompleted {
			terminalCount++
		}
		if event.Kind == protocol.EventTurnFailed {
			payload, _ := protocol.DecodeTurnFailed(event)
			t.Fatalf("turn failed: %#v", payload)
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal count = %d", terminalCount)
	}
}

func writeOpenAIAppTurn(writer io.Writer, turn int) {
	answer := fmt.Sprintf("answer-%d", turn)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-%d\"}}\n\n", turn)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n", answer)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg-%d\",\"role\":\"assistant\",\"phase\":\"final\",\"content\":[{\"type\":\"output_text\",\"text\":%q}]}}\n\n", turn, answer)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-%d\"}}\n\n", turn)
}

func writeAnthropicAppTurn(writer io.Writer, turn int) {
	answer := fmt.Sprintf("answer-%d", turn)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-%d\",\"model\":\"claude-test\"}}\n\n", turn)
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", answer)
	_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
}

func assertOpenAIThirdRequest(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Input []struct {
			Role    string `json:"role"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	input := request.Input
	if len(input) == 6 {
		if !strings.HasPrefix(input[0].Content[0].Text, "# Project instructions") {
			t.Fatalf("third OpenAI request context = %s", body)
		}
		input = input[1:]
	}
	if len(input) != 5 || input[0].Content[0].Text != "first" ||
		input[1].Content[0].Text != "answer-1" || input[2].Content[0].Text != "second" ||
		input[3].Content[0].Text != "answer-2" || input[4].Content[0].Text != "third" {
		t.Fatalf("third OpenAI request = %s", body)
	}
}

func assertAnthropicThirdRequest(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	messages := request.Messages
	if len(messages) == 6 {
		if !strings.HasPrefix(messages[0].Content[0].Text, "# Project instructions") {
			t.Fatalf("third Anthropic request context = %s", body)
		}
		messages = messages[1:]
	}
	if len(messages) != 5 || messages[0].Content[0].Text != "first" ||
		messages[1].Content[0].Text != "answer-1" || messages[2].Content[0].Text != "second" ||
		messages[3].Content[0].Text != "answer-2" || messages[4].Content[0].Text != "third" {
		t.Fatalf("third Anthropic request = %s", body)
	}
}

func TestCancelledSampleRetainsCommittedInputAfterResume(t *testing.T) {
	t.Parallel()
	requestStarted := make(chan struct{})
	cancelServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer cancelServer.Close()
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	applicationConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: cancelServer.URL,
		APIKey: secret.New("cancel-key"), Model: "gpt-test",
	}}
	created := mustOpenChatResources(t, context.Background(), applicationConfig, dataRoot, "", t.TempDir())
	if err := created.session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := created.session.Submit(context.Background(), "cancelled prompt")
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Kind != protocol.EventTurnStarted {
		t.Fatalf("first event = %s", event.Kind)
	}
	<-requestStarted
	created.session.Interrupt()
	var terminal protocol.Event
	for event := range events {
		if event.Kind == protocol.EventTurnFailed {
			terminal = event
		}
	}
	if terminal.Kind != protocol.EventTurnFailed {
		t.Fatalf("cancel terminal = %#v", terminal)
	}
	threadID := created.identity.ThreadID
	closeAppTestResources(t, created)

	requestBody := make(chan []byte, 1)
	resumeServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		requestBody <- body
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, 1)
	}))
	defer resumeServer.Close()
	applicationConfig.Provider.BaseURL = resumeServer.URL
	applicationConfig.Provider.APIKey = secret.New("refreshed-key")
	resumed := mustOpenChatResources(t, context.Background(), applicationConfig, dataRoot, string(threadID), t.TempDir())
	if len(resumed.history.Turns) != 1 || len(resumed.history.Turns[0].Messages) != 1 || resumed.history.Turns[0].Messages[0].Text != "cancelled prompt" {
		t.Fatalf("committed input missing from restored history: %#v", resumed.history)
	}
	submitAppTurn(t, resumed, "retry")
	body := <-requestBody
	if !strings.Contains(string(body), "cancelled prompt") || !strings.Contains(string(body), "retry") {
		t.Fatalf("resumed request lost committed input: %s", body)
	}
	closeAppTestResources(t, resumed)
}

func TestResumeRepairsHalfLineOnceAndKeepsCommittedHistory(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, 1)
	}))
	defer server.Close()
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	applicationConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("repair-key"), Model: "gpt-test",
	}}
	created := mustOpenChatResources(t, context.Background(), applicationConfig, dataRoot, "", t.TempDir())
	submitAppTurn(t, created, "first")
	threadID := created.identity.ThreadID
	closeAppTestResources(t, created)
	repository, err := session.OpenOrCreateRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	path, err := repository.JournalPath(threadID)
	if err != nil {
		t.Fatal(err)
	}
	_ = repository.Close()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"seq":1,"timestamp"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	resumed := mustOpenChatResources(t, context.Background(), applicationConfig, dataRoot, string(threadID), t.TempDir())
	if !resumed.repair.Repaired || resumed.repair.Kind != session.RepairHalfLine || len(resumed.history.Turns) != 1 {
		t.Fatalf("repair/history = %#v/%#v", resumed.repair, resumed.history)
	}
	closeAppTestResources(t, resumed)
	second := mustOpenChatResources(t, context.Background(), applicationConfig, dataRoot, string(threadID), t.TempDir())
	if second.repair.Repaired || len(second.history.Turns) != 1 {
		t.Fatalf("second repair/history = %#v/%#v", second.repair, second.history)
	}
	closeAppTestResources(t, second)
}

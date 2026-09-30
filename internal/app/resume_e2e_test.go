package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tui"
)

func TestProvidersPersistResumeReplayAndContinueEndToEnd(t *testing.T) {
	fixtures := []struct {
		name        string
		family      domain.ProviderFamily
		wire        string
		model       string
		serveTurn   func(io.Writer, int)
		assertThird func(*testing.T, []byte)
	}{
		{
			name: "OpenAI Responses", family: domain.ProviderOpenAI, wire: "responses", model: "gpt-test",
			serveTurn: writeOpenAIAppTurn, assertThird: assertOpenAIThirdRequest,
		},
		{
			name: "Anthropic Messages", family: domain.ProviderAnthropic, wire: "messages", model: "claude-test",
			serveTurn: writeAnthropicAppTurn, assertThird: assertAnthropicThirdRequest,
		},
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				mu.Lock()
				requests = append(requests, append([]byte(nil), body...))
				turn := len(requests)
				mu.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				fixture.serveTurn(writer, turn)
			}))
			defer server.Close()

			dataRoot := filepath.Join(t.TempDir(), "sessions")
			creationCWD := t.TempDir()
			providerConfig := config.Config{Provider: config.Provider{
				Family: fixture.family, BaseURL: server.URL,
				APIKey: secret.New("e2e-secret"), Model: fixture.model,
			}}
			created, err := openChatResources(context.Background(), providerConfig, dataRoot, "", creationCWD)
			if err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, created, "first")
			submitAppTurn(t, created, "second")
			threadID := created.identity.ThreadID
			if err := created.close(context.Background()); err != nil {
				t.Fatal(err)
			}

			resumed, err := openChatResources(
				context.Background(), providerConfig, dataRoot, string(threadID), t.TempDir(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(resumed.history.Turns) != 2 || resumed.history.Turns[0].UserText != "first" ||
				resumed.history.Turns[0].AssistantText != "answer-1" ||
				resumed.history.Turns[1].UserText != "second" || resumed.history.Turns[1].AssistantText != "answer-2" {
				t.Fatalf("resumed history = %#v", resumed.history)
			}
			view := tui.NewModel(context.Background(), "test", resumed.session, resumed.history).View()
			for _, visible := range []string{"User: first", "Assistant: answer-1", "User: second", "Assistant: answer-2"} {
				if !strings.Contains(view, visible) {
					t.Fatalf("resumed transcript missing %q: %s", visible, view)
				}
			}
			submitAppTurn(t, resumed, "third")
			if err := resumed.close(context.Background()); err != nil {
				t.Fatal(err)
			}

			mu.Lock()
			captured := make([][]byte, len(requests))
			for index, request := range requests {
				captured[index] = append([]byte(nil), request...)
			}
			mu.Unlock()
			if len(captured) != 3 {
				t.Fatalf("request count = %d", len(captured))
			}
			fixture.assertThird(t, captured[2])
			for _, dynamic := range []string{string(threadID), dataRoot, creationCWD, "e2e-secret"} {
				if strings.Contains(string(captured[2]), dynamic) {
					t.Fatalf("request contains dynamic/session input %q", dynamic)
				}
			}

			repository, err := session.OpenOrCreateRepository(dataRoot)
			if err != nil {
				t.Fatal(err)
			}
			lease, err := repository.Open(context.Background(), threadID)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := session.NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			if err := lease.Close(); err != nil {
				t.Fatal(err)
			}
			_ = repository.Close()
			if len(loaded.Records) != 11 || loaded.NextSequence != 12 {
				t.Fatalf("journal record count/next = %d/%d", len(loaded.Records), loaded.NextSequence)
			}
			for index, record := range loaded.Records {
				if record.Sequence != uint64(index+1) || record.ReplayRequirement != session.ReplayRequired {
					t.Fatalf("record[%d] = %#v", index, record)
				}
				if strings.Contains(string(record.Payload), "e2e-secret") || strings.Contains(string(record.Payload), server.URL) {
					t.Fatal("journal contains connection secret")
				}
			}

			fresh, err := openChatResources(context.Background(), providerConfig, dataRoot, "", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if fresh.identity.ThreadID == threadID || len(fresh.history.Turns) != 0 {
				t.Fatalf("non-resume startup reused history: %#v", fresh)
			}
			if err := fresh.close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenAIV1CompatibilityFixtureRestoresVisibleHistoryWithoutNetwork(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "session", "testdata", "migrations", "v1", "root.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	repository, err := session.OpenOrCreateRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	threadID := domain.ThreadID("00000000-0001-7000-8000-000000000002")
	lease, err := repository.Create(context.Background(), threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	path, err := repository.JournalPath(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	var networkCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		networkCalls.Add(1)
	}))
	defer server.Close()
	applicationConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("fixture-key"), Model: "fixture-model",
	}}
	resources, err := openChatResources(
		context.Background(), applicationConfig, dataRoot, string(threadID), t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if networkCalls.Load() != 0 {
		t.Fatalf("v1 fixture restore made %d network calls", networkCalls.Load())
	}
	if len(resources.history.Turns) != 1 ||
		resources.history.Turns[0].UserText != "fixture question" ||
		resources.history.Turns[0].AssistantText != "fixture answer" {
		t.Fatalf("v1 fixture history = %#v", resources.history)
	}
	view := tui.NewModel(context.Background(), "test", resources.session, resources.history).View()
	if !strings.Contains(view, "User: fixture question") || !strings.Contains(view, "Assistant: fixture answer") {
		t.Fatalf("v1 fixture transcript = %s", view)
	}
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBusyResumeDoesNotExposeChatResourcesOrCallProvider(t *testing.T) {
	var networkCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		networkCalls.Add(1)
	}))
	defer server.Close()
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	applicationConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: server.URL,
		APIKey: secret.New("fixture-key"), Model: "gpt-test",
	}}
	created, err := openChatResources(context.Background(), applicationConfig, dataRoot, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := session.OpenOrCreateRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	path, err := repository.JournalPath(created.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := openChatResources(
		context.Background(), applicationConfig, dataRoot, string(created.identity.ThreadID), t.TempDir(),
	)
	if resumed != nil || !errors.Is(err, &fault.Error{Code: fault.CodeSessionBusy}) {
		t.Fatalf("busy resources = %#v, %v", resumed, err)
	}
	if networkCalls.Load() != 0 {
		t.Fatalf("busy resume made %d network calls", networkCalls.Load())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("busy resource assembly modified journal bytes")
	}
	if err := created.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func submitAppTurn(t *testing.T, resources *chatResources, text string) {
	t.Helper()
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
	if len(request.Input) != 5 || request.Input[0].Content[0].Text != "first" ||
		request.Input[1].Content[0].Text != "answer-1" || request.Input[2].Content[0].Text != "second" ||
		request.Input[3].Content[0].Text != "answer-2" || request.Input[4].Content[0].Text != "third" {
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
	if len(request.Messages) != 5 || request.Messages[0].Content[0].Text != "first" ||
		request.Messages[1].Content[0].Text != "answer-1" || request.Messages[2].Content[0].Text != "second" ||
		request.Messages[3].Content[0].Text != "answer-2" || request.Messages[4].Content[0].Text != "third" {
		t.Fatalf("third Anthropic request = %s", body)
	}
}

func TestResumeWithDifferentCWDAndRefreshedConnectionDoesNotChangeProcessCWD(t *testing.T) {
	t.Parallel()
	firstServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, 1)
	}))
	defer firstServer.Close()
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	firstConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: firstServer.URL,
		APIKey: secret.New("old-key"), Model: "gpt-test",
	}}
	created, err := openChatResources(context.Background(), firstConfig, dataRoot, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	submitAppTurn(t, created, "first")
	threadID := created.identity.ThreadID
	if err := created.close(context.Background()); err != nil {
		t.Fatal(err)
	}

	requestSeen := make(chan http.Header, 1)
	secondServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestSeen <- request.Header.Clone()
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, 2)
	}))
	defer secondServer.Close()
	secondConfig := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, BaseURL: secondServer.URL,
		APIKey: secret.New("refreshed-key"), Model: "gpt-test",
	}}
	before, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := openChatResources(context.Background(), secondConfig, dataRoot, string(threadID), filepath.Join(t.TempDir(), "different"))
	if err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Abs(".")
	if before != after {
		t.Fatalf("resume changed process cwd from %q to %q", before, after)
	}
	submitAppTurn(t, resumed, "second")
	header := <-requestSeen
	if header.Get("Authorization") != "Bearer refreshed-key" {
		t.Fatalf("resume did not use refreshed credentials: %#v", header)
	}
	if err := resumed.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledTurnDoesNotEnterHistoryAfterResume(t *testing.T) {
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
	created, err := openChatResources(context.Background(), applicationConfig, dataRoot, "", t.TempDir())
	if err != nil {
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
	if err := created.close(context.Background()); err != nil {
		t.Fatal(err)
	}

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
	resumed, err := openChatResources(context.Background(), applicationConfig, dataRoot, string(threadID), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.history.Turns) != 0 {
		t.Fatalf("cancelled turn entered restored history: %#v", resumed.history)
	}
	submitAppTurn(t, resumed, "retry")
	body := <-requestBody
	if strings.Contains(string(body), "cancelled prompt") || !strings.Contains(string(body), "retry") {
		t.Fatalf("resumed request contains cancelled history: %s", body)
	}
	if err := resumed.close(context.Background()); err != nil {
		t.Fatal(err)
	}
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
	created, err := openChatResources(context.Background(), applicationConfig, dataRoot, "", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	submitAppTurn(t, created, "first")
	threadID := created.identity.ThreadID
	if err := created.close(context.Background()); err != nil {
		t.Fatal(err)
	}
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
	if _, err := file.WriteString(`{"schema_version":1,"payload_version"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	resumed, err := openChatResources(context.Background(), applicationConfig, dataRoot, string(threadID), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.repair.Repaired || resumed.repair.Kind != session.RepairHalfLine || len(resumed.history.Turns) != 1 {
		t.Fatalf("repair/history = %#v/%#v", resumed.repair, resumed.history)
	}
	if err := resumed.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, err := openChatResources(context.Background(), applicationConfig, dataRoot, string(threadID), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if second.repair.Repaired || len(second.history.Turns) != 1 {
		t.Fatalf("second repair/history = %#v/%#v", second.repair, second.history)
	}
	if err := second.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

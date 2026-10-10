package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestReadToolLoopCompletesEndToEndForBothProviders(t *testing.T) {
	for _, test := range []struct {
		name         string
		family       domain.ProviderFamily
		model        string
		serve        func(io.Writer, int)
		assertSecond func(*testing.T, []byte)
	}{
		{
			name: "OpenAI Responses", family: domain.ProviderOpenAI, model: "gpt-test",
			serve: writeOpenAIToolLoopSample, assertSecond: assertOpenAIToolContinuation,
		},
		{
			name: "Anthropic Messages", family: domain.ProviderAnthropic, model: "claude-test",
			serve: writeAnthropicToolLoopSample, assertSecond: assertAnthropicToolContinuation,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("workspace content\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var requestMu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				requestMu.Lock()
				requests = append(requests, append([]byte(nil), body...))
				sample := len(requests)
				requestMu.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				test.serve(writer, sample)
			}))
			defer server.Close()
			budget, err := contextplan.NewBudget(200000, 20000, 4096)
			if err != nil {
				t.Fatal(err)
			}
			resources, err := openChatResources(
				context.Background(),
				config.Config{
					Provider: config.Provider{
						Family: test.family, BaseURL: server.URL,
						APIKey: secret.New("tool-loop-secret"), Model: test.model,
					},
					ContextBudget: budget,
				},
				filepath.Join(t.TempDir(), "sessions"), "", workspace,
			)
			if err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, resources, "Read README.md and summarize it")
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			requestMu.Lock()
			captured := append([][]byte(nil), requests...)
			requestMu.Unlock()
			if len(captured) != 2 {
				t.Fatalf("request count = %d", len(captured))
			}
			test.assertSecond(t, captured[1])
			for _, body := range captured {
				if strings.Contains(string(body), workspace) || strings.Contains(string(body), "tool-loop-secret") {
					t.Fatal("tool request leaked workspace path or secret")
				}
			}
		})
	}
}

func TestParallelSearchToolLoopCompletesEndToEndForBothProviders(t *testing.T) {
	for _, test := range []struct {
		name   string
		family domain.ProviderFamily
		model  string
		serve  func(io.Writer, int)
		assert func(*testing.T, []byte)
	}{
		{name: "OpenAI Responses", family: domain.ProviderOpenAI, model: "gpt-test", serve: writeOpenAIParallelSearchSample, assert: assertOpenAIParallelSearchContinuation},
		{name: "Anthropic Messages", family: domain.ProviderAnthropic, model: "claude-test", serve: writeAnthropicParallelSearchSample, assert: assertAnthropicParallelSearchContinuation},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			if err := os.Mkdir(filepath.Join(workspace, "src"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, "src", "main.go"), []byte("package main\n// needle\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var requestMu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				requestMu.Lock()
				requests = append(requests, append([]byte(nil), body...))
				sample := len(requests)
				requestMu.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				test.serve(writer, sample)
			}))
			defer server.Close()
			resources, err := openChatResources(
				context.Background(), config.Config{Provider: config.Provider{
					Family: test.family, BaseURL: server.URL, APIKey: secret.New("parallel-search-secret"), Model: test.model,
				}}, filepath.Join(t.TempDir(), "sessions"), "", workspace,
			)
			if err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, resources, "Find and inspect the matching source")
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			requestMu.Lock()
			captured := append([][]byte(nil), requests...)
			requestMu.Unlock()
			if len(captured) != 2 {
				t.Fatalf("request count = %d", len(captured))
			}
			test.assert(t, captured[1])
		})
	}
}

func TestToolLoopCrashPointsReconcileLocallyForBothProviders(t *testing.T) {
	for _, test := range []struct {
		name   string
		family domain.ProviderFamily
		model  string
		serve  func(io.Writer, int)
	}{
		{name: "OpenAI Responses", family: domain.ProviderOpenAI, model: "gpt-test", serve: writeOpenAIToolLoopSample},
		{name: "Anthropic Messages", family: domain.ProviderAnthropic, model: "claude-test", serve: writeAnthropicToolLoopSample},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			readPath := filepath.Join(workspace, "README.md")
			if err := os.WriteFile(readPath, []byte("original before crash\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var requestMu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				requestMu.Lock()
				requests = append(requests, append([]byte(nil), body...))
				sample := len(requests)
				requestMu.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				if sample <= 2 {
					test.serve(writer, sample)
					return
				}
				if test.family == domain.ProviderOpenAI {
					writeOpenAIAppTurn(writer, sample)
				} else {
					writeAnthropicAppTurn(writer, sample)
				}
			}))
			defer server.Close()
			applicationConfig := config.Config{Provider: config.Provider{
				Family: test.family, BaseURL: server.URL,
				APIKey: secret.New("recovery-secret"), Model: test.model,
			}}
			dataRoot := filepath.Join(t.TempDir(), "sessions")
			created, err := openChatResources(
				context.Background(), applicationConfig, dataRoot, "", workspace,
			)
			if err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, created, "Read README.md")
			threadID := created.identity.ThreadID
			journalPath, err := created.service.repository.JournalPath(threadID)
			if err != nil {
				t.Fatal(err)
			}
			if err := created.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			baseline, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			checkpoints := toolCrashCheckpoints(t, dataRoot, threadID)
			if err := os.WriteFile(readPath, []byte("changed after crash\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			for _, checkpoint := range checkpoints {
				t.Run(checkpoint.name, func(t *testing.T) {
					writeJournalPrefix(t, journalPath, baseline, checkpoint.sequence)
					requestMu.Lock()
					beforeResume := len(requests)
					requestMu.Unlock()
					resumed, err := openChatResources(
						context.Background(), applicationConfig, dataRoot, string(threadID), workspace,
					)
					if err != nil {
						t.Fatal(err)
					}
					requestMu.Lock()
					afterResume := len(requests)
					requestMu.Unlock()
					if afterResume != beforeResume {
						t.Fatalf("resume made Provider requests: before=%d after=%d", beforeResume, afterResume)
					}
					reconciledJournal, err := os.ReadFile(journalPath)
					if err != nil {
						t.Fatal(err)
					}
					submitAppTurn(t, resumed, "continue after recovery")
					if err := resumed.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					requestMu.Lock()
					if len(requests) != beforeResume+1 {
						t.Fatalf("next input request count = %d, want %d", len(requests), beforeResume+1)
					}
					directRequest := append([]byte(nil), requests[len(requests)-1]...)
					requestMu.Unlock()
					if err := os.WriteFile(journalPath, reconciledJournal, 0o600); err != nil {
						t.Fatal(err)
					}
					restarted, err := openChatResources(
						context.Background(), applicationConfig, dataRoot, string(threadID), workspace,
					)
					if err != nil {
						t.Fatal(err)
					}
					requestMu.Lock()
					beforeRestartedInput := len(requests)
					requestMu.Unlock()
					submitAppTurn(t, restarted, "continue after recovery")
					if err := restarted.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					requestMu.Lock()
					if len(requests) != beforeRestartedInput+1 {
						t.Fatalf("restarted input request count = %d, want %d", len(requests), beforeRestartedInput+1)
					}
					restartedRequest := append([]byte(nil), requests[len(requests)-1]...)
					requestMu.Unlock()
					if !bytes.Equal(directRequest, restartedRequest) {
						t.Fatalf("direct and restarted reconciled requests differ:\n%s\n%s", directRequest, restartedRequest)
					}
					body := string(restartedRequest)
					if !strings.Contains(body, "continue after recovery") ||
						!strings.Contains(body, checkpoint.outputMarker) ||
						strings.Contains(body, "changed after crash") {
						t.Fatalf("recovered request does not preserve local compensation: %s", body)
					}
					_, plan := loadToolLoopPlan(t, dataRoot, threadID)
					if plan.ToolRecovery != nil || plan.InterruptedTail != nil || len(plan.Turns) != 2 ||
						plan.Turns[0].State != session.ReplayedTurnFailed ||
						plan.Turns[1].State != session.ReplayedTurnCompleted {
						t.Fatalf("reconciled replay plan = %#v", plan)
					}
				})
			}
		})
	}
}

type toolCrashCheckpoint struct {
	name         string
	sequence     uint64
	outputMarker string
}

func toolCrashCheckpoints(t *testing.T, dataRoot string, threadID domain.ThreadID) []toolCrashCheckpoint {
	t.Helper()
	loaded, _ := loadToolLoopPlan(t, dataRoot, threadID)
	checkpoints := make([]toolCrashCheckpoint, 0, 4)
	nativeCommits := 0
	for _, record := range loaded.Records {
		switch record.EventKind {
		case session.EventToolCallReady:
			checkpoints = append(checkpoints, toolCrashCheckpoint{
				name: "ready", sequence: record.Sequence,
				outputMarker: "session interrupted before execution",
			})
		case session.EventToolExecutionStarted:
			checkpoints = append(checkpoints, toolCrashCheckpoint{
				name: "started", sequence: record.Sequence, outputMarker: "execution outcome is uncertain",
			})
		case session.EventToolCallResult:
			checkpoints = append(checkpoints, toolCrashCheckpoint{
				name: "result", sequence: record.Sequence, outputMarker: "original before crash",
			})
		case session.EventProviderNativeCommit:
			nativeCommits++
			if nativeCommits == 2 {
				checkpoints = append(checkpoints, toolCrashCheckpoint{
					name: "output", sequence: record.Sequence, outputMarker: "original before crash",
				})
			}
		}
	}
	if len(checkpoints) != 4 {
		t.Fatalf("tool checkpoints = %#v", checkpoints)
	}
	return checkpoints
}

func writeJournalPrefix(t *testing.T, path string, journal []byte, sequence uint64) {
	t.Helper()
	lines := bytes.SplitAfter(journal, []byte("\n"))
	if sequence == 0 || int(sequence) > len(lines) {
		t.Fatalf("journal prefix sequence = %d, lines = %d", sequence, len(lines))
	}
	if err := os.WriteFile(path, bytes.Join(lines[:sequence], nil), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadToolLoopPlan(
	t *testing.T,
	dataRoot string,
	threadID domain.ThreadID,
) (session.LoadResult, session.ReplayPlan) {
	t.Helper()
	repository, err := session.OpenOrCreateRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	lease, err := repository.Open(context.Background(), threadID)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := session.NewLoader().Load(context.Background(), lease)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	plan, err := session.NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	return loaded, plan
}

func writeOpenAIToolLoopSample(writer io.Writer, sample int) {
	if sample == 1 {
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-tool\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc-1\",\"call_id\":\"call-1\",\"name\":\"Read\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc-1\",\"output_index\":0,\"delta\":\"{\\\"file_path\\\":\\\"README.md\\\"}\"}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.function_call_arguments.done\",\"item_id\":\"fc-1\",\"output_index\":0,\"arguments\":\"{\\\"file_path\\\":\\\"README.md\\\"}\"}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc-1\",\"call_id\":\"call-1\",\"name\":\"Read\",\"arguments\":\"{\\\"file_path\\\":\\\"README.md\\\"}\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-tool\"}}\n\n")
		return
	}
	writeOpenAIAppTurn(writer, sample)
}

func writeAnthropicToolLoopSample(writer io.Writer, sample int) {
	if sample == 1 {
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-tool\",\"model\":\"claude-test\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu-1\",\"name\":\"Read\",\"input\":{}}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"file_path\\\":\\\"README.md\\\"}\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
		return
	}
	writeAnthropicAppTurn(writer, sample)
}

func writeOpenAIParallelSearchSample(writer io.Writer, sample int) {
	if sample != 1 {
		writeOpenAIAppTurn(writer, sample)
		return
	}
	_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-search\"}}\n\n")
	writeOpenAIFunctionCall(writer, 0, "fc-glob", "call-glob", "Glob", `{"pattern":"**/*.go"}`)
	writeOpenAIFunctionCall(writer, 1, "fc-grep", "call-grep", "Grep", `{"pattern":"needle","glob":"**/*.go","output_mode":"content"}`)
	writeOpenAIFunctionCall(writer, 2, "fc-read", "call-read", "Read", `{"file_path":"src/main.go"}`)
	_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-search\"}}\n\n")
}

func writeOpenAIFunctionCall(writer io.Writer, index int, itemID string, callID string, name string, arguments string) {
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.output_item.added\",\"output_index\":%d,\"item\":{\"type\":\"function_call\",\"id\":%q,\"call_id\":%q,\"name\":%q}}\n\n", index, itemID, callID, name)
	encodedArguments, _ := json.Marshal(arguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":%q,\"output_index\":%d,\"delta\":%s}\n\n", itemID, index, encodedArguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.function_call_arguments.done\",\"item_id\":%q,\"output_index\":%d,\"arguments\":%s}\n\n", itemID, index, encodedArguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":{\"type\":\"function_call\",\"id\":%q,\"call_id\":%q,\"name\":%q,\"arguments\":%s}}\n\n", index, itemID, callID, name, encodedArguments)
}

func writeAnthropicParallelSearchSample(writer io.Writer, sample int) {
	if sample != 1 {
		writeAnthropicAppTurn(writer, sample)
		return
	}
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-search\",\"model\":\"claude-test\"}}\n\n")
	writeAnthropicToolUse(writer, 0, "toolu-glob", "Glob", `{"pattern":"**/*.go"}`)
	writeAnthropicToolUse(writer, 1, "toolu-grep", "Grep", `{"pattern":"needle","glob":"**/*.go","output_mode":"content"}`)
	writeAnthropicToolUse(writer, 2, "toolu-read", "Read", `{"file_path":"src/main.go"}`)
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
}

func writeAnthropicToolUse(writer io.Writer, index int, callID string, name string, arguments string) {
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":%q,\"input\":{}}}\n\n", index, callID, name)
	encodedArguments, _ := json.Marshal(arguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", index, encodedArguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", index)
}

func assertOpenAIParallelSearchContinuation(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Input []struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
			Output string `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Input) != 7 {
		t.Fatalf("OpenAI parallel continuation = %s", body)
	}
	for index, want := range []struct{ callID, name string }{{"call-glob", "Glob"}, {"call-grep", "Grep"}, {"call-read", "Read"}} {
		if request.Input[index+1].Type != "function_call" || request.Input[index+1].CallID != want.callID || request.Input[index+1].Name != want.name ||
			request.Input[index+4].Type != "function_call_output" || request.Input[index+4].CallID != want.callID {
			t.Fatalf("OpenAI call/output[%d] pairing = %s", index, body)
		}
	}
	if !strings.Contains(request.Input[4].Output, "src/main.go") || !strings.Contains(request.Input[5].Output, "needle") ||
		!strings.Contains(request.Input[6].Output, "package main") {
		t.Fatalf("OpenAI search outputs = %s", body)
	}
}

func assertAnthropicParallelSearchContinuation(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Messages []struct {
			Content []struct {
				Type      string `json:"type"`
				ID        string `json:"id"`
				Name      string `json:"name"`
				ToolUseID string `json:"tool_use_id"`
				Content   string `json:"content"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 3 || len(request.Messages[1].Content) != 3 || len(request.Messages[2].Content) != 3 {
		t.Fatalf("Anthropic parallel continuation = %s", body)
	}
	for index, want := range []struct{ callID, name string }{{"toolu-glob", "Glob"}, {"toolu-grep", "Grep"}, {"toolu-read", "Read"}} {
		call := request.Messages[1].Content[index]
		result := request.Messages[2].Content[index]
		if call.Type != "tool_use" || call.ID != want.callID || call.Name != want.name ||
			result.Type != "tool_result" || result.ToolUseID != want.callID {
			t.Fatalf("Anthropic call/output[%d] pairing = %s", index, body)
		}
	}
	if !strings.Contains(request.Messages[2].Content[0].Content, "src/main.go") ||
		!strings.Contains(request.Messages[2].Content[1].Content, "needle") ||
		!strings.Contains(request.Messages[2].Content[2].Content, "package main") {
		t.Fatalf("Anthropic search outputs = %s", body)
	}
}

func assertOpenAIToolContinuation(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Input []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			CallID  string `json:"call_id"`
			Output  string `json:"output"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
		Tools []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Tools) != 3 || request.Tools[0].Type != "function" || request.Tools[0].Name != "Glob" ||
		request.Tools[1].Type != "function" || request.Tools[1].Name != "Grep" ||
		request.Tools[2].Type != "function" || request.Tools[2].Name != "Read" ||
		len(request.Input) != 3 {
		t.Fatalf("OpenAI continuation shape = %s", body)
	}
	var output struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(request.Input[2].Output), &output); err != nil {
		t.Fatalf("decode OpenAI tool output: %v", err)
	}
	if request.Input[0].Role != "user" || request.Input[0].Content[0].Text != "Read README.md and summarize it" ||
		request.Input[1].Type != "function_call" || request.Input[1].CallID != "call-1" ||
		request.Input[2].Type != "function_call_output" || request.Input[2].CallID != "call-1" ||
		!strings.Contains(output.Content, "1\tworkspace content") {
		t.Fatalf("OpenAI continuation pairing = %s", body)
	}
}

func assertAnthropicToolContinuation(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				ID        string `json:"id"`
				ToolUseID string `json:"tool_use_id"`
				Content   string `json:"content"`
			} `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.Tools) != 3 || request.Tools[0].Name != "Glob" || request.Tools[1].Name != "Grep" ||
		request.Tools[2].Name != "Read" || len(request.Messages) != 3 {
		t.Fatalf("Anthropic continuation shape = %s", body)
	}
	if request.Messages[0].Role != "user" || request.Messages[0].Content[0].Text != "Read README.md and summarize it" ||
		request.Messages[1].Role != "assistant" || request.Messages[1].Content[0].Type != "tool_use" ||
		request.Messages[1].Content[0].ID != "toolu-1" || request.Messages[2].Role != "user" ||
		request.Messages[2].Content[0].Type != "tool_result" ||
		request.Messages[2].Content[0].ToolUseID != "toolu-1" ||
		!strings.Contains(request.Messages[2].Content[0].Content, "1\tworkspace content") {
		t.Fatalf("Anthropic continuation pairing = %s", body)
	}
}

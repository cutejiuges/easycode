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
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestInteractivePatchDenyFeedbackAndConflictForBothProviders(t *testing.T) {
	providers := []struct {
		name   string
		family domain.ProviderFamily
		model  string
		serve  func(io.Writer, int)
	}{
		{name: "OpenAI Responses", family: domain.ProviderOpenAI, model: "gpt-test", serve: writeOpenAIPatchSample},
		{name: "Anthropic Messages", family: domain.ProviderAnthropic, model: "claude-test", serve: writeAnthropicPatchSample},
	}
	for _, providerCase := range providers {
		for _, scenario := range []string{"deny feedback", "conflict"} {
			t.Run(providerCase.name+"/"+scenario, func(t *testing.T) {
				workspace := t.TempDir()
				path := filepath.Join(workspace, "main.go")
				writeAppTestFile(t, path, []byte("old\n"), 0o600)
				var recorder requestRecorder
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					requestIndex, _ := recorder.record(t, request)
					sample := requestIndex + 1
					writer.Header().Set("Content-Type", "text/event-stream")
					providerCase.serve(writer, sample)
				}))
				defer server.Close()
				resources := mustOpenChatResourcesWithHooks(t,
					context.Background(), config.Config{Provider: config.Provider{
						Family: providerCase.family, BaseURL: server.URL, APIKey: secret.New("patch-scenario-secret"), Model: providerCase.model,
					}}, dataPaths{sessionRoot: filepath.Join(t.TempDir(), "sessions")}, "", false, workspace,
					chatResourceHooks{interactiveApprovals: true},
				)
				controlSession, err := startTUIControlSession(context.Background(), resources.runtime)
				if err != nil {
					t.Fatal(err)
				}
				events, err := submitInteractiveFixture(t, controlSession, context.Background(), "Update main.go")
				if err != nil {
					t.Fatal(err)
				}
				for event := range events {
					if event.Kind != protocol.EventToolApprovalRequested {
						continue
					}
					payload, decodeErr := protocol.DecodeToolApprovalRequested(event)
					if decodeErr != nil {
						t.Fatal(decodeErr)
					}
					decision := protocol.ApprovalDeny
					feedback := "keep the old name"
					if scenario == "conflict" {
						writeAppTestFile(t, path, []byte("external\n"), 0o600)
						decision = protocol.ApprovalAllowOnce
						feedback = ""
					}
					if resolveErr := controlSession.ResolveApproval(context.Background(), event.TurnID, payload, decision, feedback); resolveErr != nil {
						t.Fatal(resolveErr)
					}
				}
				if err := controlSession.Shutdown(context.Background()); err != nil {
					t.Fatal(err)
				}
				closeAppTestResources(t, resources)
				content, _ := os.ReadFile(path)
				captured := recorder.snapshot()
				wantContent, wantOutput := "old\n", "keep the old name"
				if scenario == "conflict" {
					wantContent, wantOutput = "external\n", "patch_conflict"
				}
				if string(content) != wantContent || len(captured) != 2 || !strings.Contains(string(captured[1]), wantOutput) {
					t.Fatalf("content=%q continuation=%s", content, captured[len(captured)-1])
				}
			})
		}
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
			writeAppTestFile(t, readPath, []byte("original before crash\n"), 0o600)
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
			created := mustOpenChatResources(t,
				context.Background(), applicationConfig, dataRoot, "", workspace,
			)
			submitAppTurn(t, created, "Read README.md")
			threadID := created.identity.ThreadID
			journalPath, err := created.service.repository.JournalPath(threadID)
			if err != nil {
				t.Fatal(err)
			}
			closeAppTestResources(t, created)
			baseline := readAppTestFile(t, journalPath)
			checkpoints := toolCrashCheckpoints(t, dataRoot, threadID)
			writeAppTestFile(t, readPath, []byte("changed after crash\n"), 0o600)

			for _, checkpoint := range checkpoints {
				t.Run(checkpoint.name, func(t *testing.T) {
					writeJournalPrefix(t, journalPath, baseline, checkpoint.sequence)
					requestMu.Lock()
					beforeResume := len(requests)
					requestMu.Unlock()
					resumed := mustOpenChatResources(t,
						context.Background(), applicationConfig, dataRoot, string(threadID), workspace,
					)
					requestMu.Lock()
					afterResume := len(requests)
					requestMu.Unlock()
					if afterResume != beforeResume {
						t.Fatalf("resume made Provider requests: before=%d after=%d", beforeResume, afterResume)
					}
					reconciledJournal := readAppTestFile(t, journalPath)
					submitAppTurn(t, resumed, "continue after recovery")
					closeAppTestResources(t, resumed)
					requestMu.Lock()
					if len(requests) != beforeResume+1 {
						t.Fatalf("next input request count = %d, want %d", len(requests), beforeResume+1)
					}
					directRequest := append([]byte(nil), requests[len(requests)-1]...)
					requestMu.Unlock()
					writeAppTestFile(t, journalPath, reconciledJournal, 0o600)
					restarted := mustOpenChatResources(t,
						context.Background(), applicationConfig, dataRoot, string(threadID), workspace,
					)
					requestMu.Lock()
					beforeRestartedInput := len(requests)
					requestMu.Unlock()
					submitAppTurn(t, restarted, "continue after recovery")
					closeAppTestResources(t, restarted)
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
			if nativeCommits == 3 {
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
	writeAppTestFile(t, path, bytes.Join(lines[:sequence], nil), 0o600)
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

func writeOpenAIPatchSample(writer io.Writer, sample int) {
	if sample != 1 {
		writeOpenAIAppTurn(writer, sample)
		return
	}
	patch := "*** Begin Patch\n*** Update File: main.go\n@@\n-old\n+new\n*** End Patch\n"
	encoded, _ := json.Marshal(patch)
	_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-patch\"}}\n\n")
	_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"custom_tool_call\",\"id\":\"ctc-1\",\"call_id\":\"call-patch\",\"name\":\"apply_patch\"}}\n\n")
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.custom_tool_call_input.delta\",\"item_id\":\"ctc-1\",\"output_index\":0,\"delta\":%s}\n\n", encoded)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.custom_tool_call_input.done\",\"item_id\":\"ctc-1\",\"output_index\":0,\"input\":%s}\n\n", encoded)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"custom_tool_call\",\"id\":\"ctc-1\",\"call_id\":\"call-patch\",\"name\":\"apply_patch\",\"input\":%s}}\n\n", encoded)
	_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-patch\"}}\n\n")
}

func writeAnthropicPatchSample(writer io.Writer, sample int) {
	if sample != 1 {
		writeAnthropicAppTurn(writer, sample)
		return
	}
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-patch\",\"model\":\"claude-test\"}}\n\n")
	writeAnthropicToolUse(writer, 0, "toolu-edit", "Edit", `{"file_path":"main.go","old_string":"old","new_string":"new"}`)
	_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
}

func writeOpenAIFunctionCall(writer io.Writer, index int, itemID string, callID string, name string, arguments string) {
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.output_item.added\",\"output_index\":%d,\"item\":{\"type\":\"function_call\",\"id\":%q,\"call_id\":%q,\"name\":%q}}\n\n", index, itemID, callID, name)
	encodedArguments, _ := json.Marshal(arguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":%q,\"output_index\":%d,\"delta\":%s}\n\n", itemID, index, encodedArguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.function_call_arguments.done\",\"item_id\":%q,\"output_index\":%d,\"arguments\":%s}\n\n", itemID, index, encodedArguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":{\"type\":\"function_call\",\"id\":%q,\"call_id\":%q,\"name\":%q,\"arguments\":%s}}\n\n", index, itemID, callID, name, encodedArguments)
}

func writeAnthropicToolUse(writer io.Writer, index int, callID string, name string, arguments string) {
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":%q,\"input\":{}}}\n\n", index, callID, name)
	encodedArguments, _ := json.Marshal(arguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", index, encodedArguments)
	_, _ = fmt.Fprintf(writer, "data: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", index)
}

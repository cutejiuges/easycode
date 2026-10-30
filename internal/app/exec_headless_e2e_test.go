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
	"testing"

	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/session"
)

func writeExecSample(writer io.Writer, family domain.ProviderFamily, sample int) {
	if sample != 1 {
		if family == domain.ProviderOpenAI {
			writeOpenAIAppTurn(writer, sample)
		} else {
			writeAnthropicAppTurn(writer, sample)
		}
		return
	}
	writeExecCommandSample(writer, family, "printf forbidden > exec-marker", 1000)
}

func writeExecCommandSample(writer io.Writer, family domain.ProviderFamily, command string, timeout int) {
	encoded, _ := json.Marshal(struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout_ms"`
	}{command, timeout})
	arguments := string(encoded)
	if family == domain.ProviderOpenAI {
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-exec\"}}\n\n")
		writeOpenAIFunctionCall(writer, 0, "fc-exec", "call-exec", "exec_command", arguments)
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-exec\"}}\n\n")
	} else {
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-exec\",\"model\":\"claude-test\"}}\n\n")
		writeAnthropicToolUse(writer, 0, "call-exec", "Bash", arguments)
		_, _ = io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
	}
}

func TestExecHeadlessModesFailClosedForBothProviders(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(fmt.Sprintf("%s/mode-%d", family, mode), func(t *testing.T) {
				workspace := t.TempDir()
				t.Chdir(workspace)
				var mu sync.Mutex
				var requests [][]byte
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					body, err := io.ReadAll(request.Body)
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					requests = append(requests, body)
					sample := len(requests)
					mu.Unlock()
					writer.Header().Set("Content-Type", "text/event-stream")
					writeExecSample(writer, family, sample)
				}))
				defer server.Close()
				root := filepath.Join(privateAppTempDir(t), "sessions")
				output := &appWriteCloser{}
				options := Options{Mode: mode, ConfigPath: writeAppConfig(t, t.TempDir(), family, server.URL), SessionDataRoot: root, Output: output}
				if mode == headless.ModeStreamJSON {
					options.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"exec-request\",\"text\":\"run command\"}\n"))
				} else {
					options.Prompt = "run command"
				}
				outcome := Run(context.Background(), options)
				if outcome.ExitCode() != 0 {
					t.Fatalf("headless failed: %#v output=%s", outcome, output.String())
				}
				mu.Lock()
				captured := append([][]byte(nil), requests...)
				mu.Unlock()
				if len(captured) != 2 {
					t.Fatalf("request count = %d", len(captured))
				}
				wire := "function_call_output"
				if family == domain.ProviderAnthropic {
					wire = "tool_result"
				}
				continuation := string(captured[1])
				if !strings.Contains(continuation, wire) || !strings.Contains(continuation, "approval_required") || !strings.Contains(continuation, "call-exec") {
					t.Fatalf("denial not paired: %s", continuation)
				}
				if _, err := os.Stat(filepath.Join(workspace, "exec-marker")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("headless command produced side effect")
				}
				for _, forbidden := range []string{"tool_started", "tool_progress", "tool_terminal", workspace} {
					if strings.Contains(output.String(), forbidden) {
						t.Fatalf("headless exposed %q", forbidden)
					}
				}
				if mode != headless.ModeText {
					started := decodeThreadStarted(t, output.String())
					loaded := loadHeadlessJournal(t, root, started.ThreadID)
					var denied int
					for _, record := range loaded.Records {
						if record.EventKind == session.EventToolExecutionStarted {
							t.Fatal("headless admitted process execution")
						}
						if record.EventKind == session.EventToolCallResult {
							result, err := session.DecodeToolCallResultPayload(record)
							if err != nil || result.Code != "approval_required" {
								t.Fatalf("unexpected result: %#v %v", result, err)
							}
							denied++
						}
					}
					if denied != 1 {
						t.Fatalf("denial count = %d", denied)
					}
				}
			})
		}
	}
}

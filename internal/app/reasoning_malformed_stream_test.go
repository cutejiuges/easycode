package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/session"
)

func duplicateFinalReasoningUsage(t *testing.T, family domain.ProviderFamily, wire []byte) []byte {
	t.Helper()
	field := "reasoning_tokens"
	if family == domain.ProviderAnthropic {
		field = "thinking_tokens"
	}
	member := []byte(fmt.Sprintf(`"output_tokens_details":{"%s":30}`, field))
	position := bytes.LastIndex(wire, member)
	if position < 0 {
		t.Fatal("fixture has no final reasoning usage")
	}
	invalid := []byte(fmt.Sprintf(`"private_note":"usage-private-sentinel","output_tokens_details":{"%s":null},"output_tokens_details":{"%s":30}`, field, field))
	result := append([]byte(nil), wire[:position]...)
	result = append(result, invalid...)
	return append(result, wire[position+len(member):]...)
}

func TestAmbiguousReasoningUsageNeverCommitsSampleOrRunsTool(t *testing.T) {
	clearProviderEnvironment(t)
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(fmt.Sprintf("%s/%d", family, mode), func(t *testing.T) {
				workspace := t.TempDir()
				t.Chdir(workspace)
				if err := os.WriteFile("README.md", []byte("tool must not run"), 0600); err != nil {
					t.Fatal(err)
				}
				wire := duplicateFinalReasoningUsage(t, family, activeReasoningFixture(family, 1, true))
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write(wire)
				}))
				defer server.Close()
				path := withReasoningConfig(t, writeAppConfig(t, t.TempDir(), family, server.URL), reasoningFields(family))
				root := filepath.Join(privateAppTempDir(t), "sessions")
				output := &appWriteCloser{}
				opts := Options{Mode: mode, Prompt: "Read README.md", ConfigPath: path, SessionDataRoot: root, Output: output}
				if mode == headless.ModeStreamJSON {
					opts.Prompt = ""
					opts.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"invalid-usage\",\"text\":\"Read README.md\"}\n"))
				}
				outcome := Run(context.Background(), opts)
				if mode == headless.ModeStreamJSON {
					// 长会话允许单个 turn 失败后正常关闭，失败必须通过关联事件交付。
					if outcome.ExitCode() != 0 || !strings.Contains(output.String(), `"type":"turn.failed"`) || !strings.Contains(output.String(), `"code":"stream_protocol_error"`) || strings.Contains(output.String(), `"type":"turn.completed"`) {
						t.Fatalf("stream-json lost turn failure: %+v output=%s", outcome, output.String())
					}
				} else if outcome.ExitCode() != 1 || outcome.Failure.Code != fault.CodeStreamProtocol {
					t.Fatalf("ambiguous stream succeeded or retried: %+v requests=%d", outcome, requests.Load())
				}
				if requests.Load() != 1 {
					t.Fatal("malformed response triggered a retry")
				}
				for _, private := range []string{"usage-private-sentinel", "allowed-reasoning-sentinel", "opaque-reasoning-sentinel"} {
					if strings.Contains(output.String()+outcome.Failure.Message, private) {
						t.Fatal("malformed response leaked private content")
					}
				}
				repository, err := session.OpenOrCreateRepository(root)
				if err != nil {
					t.Fatal(err)
				}
				locations, err := repository.EnumerateJournals(context.Background())
				if closeErr := repository.Close(); err != nil || closeErr != nil || len(locations) != 1 {
					t.Fatalf("journal enumeration: %v %v %d", err, closeErr, len(locations))
				}
				loaded := loadHeadlessJournal(t, root, locations[0].ThreadID)
				plan, err := session.NewReplayPlanner().Plan(loaded)
				if err != nil {
					t.Fatal(err)
				}
				if len(plan.SampleUsages) != 0 || len(plan.Tools) != 0 || len(plan.NativeCommits) != 1 || loaded.Records[len(loaded.Records)-1].EventKind != session.EventTurnFailed {
					t.Fatal("invalid usage became durable sample, tool execution, or success")
				}
			})
		}
	}
}

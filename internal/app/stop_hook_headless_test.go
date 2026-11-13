package app

import (
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
	"easycode/internal/headless"
	"easycode/internal/session"
)

func TestStopHeadlessModesKeepSavedAnswerAndRequireTrust(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, mode := range []headless.Mode{headless.ModeText, headless.ModeJSON, headless.ModeStreamJSON} {
			t.Run(fmt.Sprintf("%s/mode-%d", family, mode), func(t *testing.T) {
				workspace := t.TempDir()
				t.Chdir(workspace)
				writeStopHookConfig(t, workspace, "printf forbidden > hook-marker", domain.HookFeedbackModel)
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Content-Type", "text/event-stream")
					serveStopAssistant(w, family, "saved answer")
				}))
				defer server.Close()
				root := filepath.Join(t.TempDir(), "sessions")
				output := &appWriteCloser{}
				options := Options{Mode: mode, Prompt: "complete task", ConfigPath: writeAppConfig(t, t.TempDir(), family, server.URL), SessionDataRoot: root, Output: output}
				if mode == headless.ModeStreamJSON {
					options.Prompt = ""
					options.Input = io.NopCloser(strings.NewReader("{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"stop-request\",\"text\":\"complete task\"}\n"))
				}
				outcome := Run(context.Background(), options)
				if (outcome.ExitCode() == 0 && mode != headless.ModeStreamJSON) || requests.Load() != 1 {
					t.Fatalf("headless bypassed Stop: %#v requests=%d output=%s", outcome, requests.Load(), output.String())
				}
				if mode != headless.ModeText && !strings.Contains(output.String(), "hook_trust_required") {
					t.Fatal("missing safe reason", output.String())
				}
				for _, private := range []string{"stop_hook_committed", "hook_execution", "hook-marker", workspace} {
					if strings.Contains(output.String(), private) {
						t.Fatal("internal hook presentation leaked", private)
					}
				}
				if _, err := os.Stat(filepath.Join(workspace, "hook-marker")); !os.IsNotExist(err) {
					t.Fatal("headless executed script")
				}
				if mode != headless.ModeText {
					started := decodeThreadStarted(t, output.String())
					loaded := loadHeadlessJournal(t, root, started.ThreadID)
					plan, err := session.NewReplayPlanner().Plan(loaded)
					if err != nil {
						t.Fatal(err)
					}
					if plan.StopRecovery != nil || len(plan.Stops) != 1 || plan.Stops[0].Decision.Code != domain.HookTrustRequired || plan.Stops[0].Decision.Processed != 0 || len(plan.NativeCommits) != 2 || len(plan.SampleUsages) != 1 {
						t.Fatal("headless lost saved sample or started execution")
					}
				}
			})
		}
	}
}

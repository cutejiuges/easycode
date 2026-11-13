package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/headless"
)

// TestStopTerminalHelper 仅由实际 PTY 脚本显式启用；所有 Provider 响应来自本地服务。
func TestStopTerminalHelper(t *testing.T) {
	name := os.Getenv("EASYCODE_STOP_TERMINAL_SMOKE")
	if name == "" {
		return
	}
	family := domain.ProviderFamily(name)
	if !family.Valid() {
		t.Fatal("invalid terminal provider")
	}
	clearProviderEnvironment(t)
	workspace := t.TempDir()
	t.Chdir(workspace)
	writeStopHookConfig(t, workspace, `payload=$(cat); case "$payload" in *cancel-completion*) /bin/sleep 5 ;; *'"stop_hook_active":true'*) printf '{}' ;; *) printf '%s' '{"decision":"block","reason":"PTY authorized completion feedback"}' ;; esac`, domain.HookFeedbackModel)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		text := "cancel-completion"
		switch n {
		case 1:
			text = "stop-first-answer"
		case 2:
			text = "stop-needs-fix"
		case 3:
			text = "stop-fixed-answer"
		}
		serveStopAssistant(w, family, text)
	}))
	defer server.Close()
	configuration := writeAppConfig(t, t.TempDir(), family, server.URL)
	root := filepath.Join(privateAppTempDir(t), "sessions")
	outcome := Run(context.Background(), Options{Mode: headless.ModeInteractive, ConfigPath: configuration, SessionDataRoot: root, Input: os.Stdin, Output: os.Stdout})
	fmt.Fprintf(os.Stdout, "\nSTOP_TERMINAL_EXIT=%d REQUESTS=%d\n", outcome.ExitCode(), calls.Load())
	if outcome.ExitCode() != 0 || calls.Load() != 4 {
		t.Fatal("unexpected terminal result")
	}
}

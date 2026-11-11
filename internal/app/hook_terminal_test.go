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
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// TestHookTerminalHelper 由实际 PTY 验收显式启用，普通质量门不打开终端。
func TestHookTerminalHelper(t *testing.T) {
	name := os.Getenv("EASYCODE_HOOK_TERMINAL_SMOKE")
	if name == "" {
		return
	}
	family := domain.ProviderFamily(name)
	if !family.Valid() {
		t.Fatal("invalid terminal fixture provider")
	}
	for _, key := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(key, "")
	}
	event := domain.HookEvent(os.Getenv("EASYCODE_HOOK_TERMINAL_EVENT"))
	if event == "" {
		event = domain.HookPreToolUse
	}
	if !event.Valid() {
		t.Fatal("invalid terminal hook event")
	}
	workspace := t.TempDir()
	t.Chdir(workspace)
	if event == domain.HookPostToolUseFailure {
		writeFailureHookConfig(t, workspace, `payload=$(cat); case "$payload" in *cancel*) /bin/sleep 5;; *) printf '%s' '{"hookSpecificOutput":{"hookEventName":"PostToolUseFailure","additionalContext":"Authorized failure diagnosis: inspect original conflict."}}' ;; esac`, domain.HookFeedbackModel)
		fmt.Fprintf(os.Stdout, "HOOK_TERMINAL_WORKSPACE=%s\n", workspace)
	} else if event == domain.HookPostToolUse {
		if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("// TODO\n"), 0600); err != nil {
			t.Fatal(err)
		}
		writePostHookConfig(t, workspace, `payload=$(cat); case "$payload" in *cancel*) /bin/sleep 5;; *) `+postReadOnlyExample()+` ;; esac`, domain.HookFeedbackModel)
	} else {
		writeHookConfig(t, workspace, `payload=$(cat); case "$payload" in *cancel*) /bin/sleep 5;; *) printf '{}' ;; esac`)
	}
	hookPath := filepath.Join(workspace, ".easycode", "hooks.json")
	hookBytes, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	hookBytes = bytes.Replace(hookBytes, []byte(`"timeout":1`), []byte(`"timeout":5`), 1)
	if err := os.WriteFile(hookPath, hookBytes, 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		n := calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		var tools []writeScenarioCall
		if n%2 == 1 {
			content := "reviewed"
			if n == 5 {
				content = "cancel"
			}
			path := "terminal-marker"
			if n == 5 && event.Post() {
				path = "terminal-cancel"
			}
			tools = []writeScenarioCall{{"Write", fmt.Sprintf(`{"file_path":%q,"content":%q}`, path, content)}}
		}
		serveWriteScenarioSample(w, family, int(n), tools)
	}))
	defer server.Close()
	configuration := writeAppConfig(t, t.TempDir(), family, server.URL)
	sessionRoot := filepath.Join(privateAppTempDir(t), "sessions")
	outcome := Run(context.Background(), Options{Mode: headless.ModeInteractive, ConfigPath: configuration, SessionDataRoot: sessionRoot, Input: os.Stdin, Output: os.Stdout})
	if event == domain.HookPostToolUseFailure {
		assertFailureTerminalFacts(t, sessionRoot)
	}
	fmt.Fprintf(os.Stdout, "\nHOOK_TERMINAL_EXIT=%d REQUESTS=%d\n", outcome.ExitCode(), calls.Load())
}

// assertFailureTerminalFacts 在真实终端退出后读取生产回放，核验原冲突与取消终态。
func assertFailureTerminalFacts(t *testing.T, root string) {
	t.Helper()
	var thread domain.ThreadID
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".jsonl" {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			record, err := session.DecodeRecord(bytes.SplitN(b, []byte("\n"), 2)[0])
			if err != nil {
				return err
			}
			thread = record.ThreadID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	repo, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Open(context.Background(), thread)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	loaded, err := session.NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := session.NewReplayPlanner().Plan(loaded)
	if err != nil || len(plan.Tools) != 3 {
		t.Fatal("terminal lost original calls", err)
	}
	last := plan.Tools[2]
	if !last.HasResult || last.Result.Status() != tool.ResultConflict || last.PostHooks == nil || last.PostHooks.Terminal == nil || last.PostHooks.Terminal.Route != domain.PostHookRouteFailure || last.PostHooks.Terminal.Disposition != domain.PostHookCancelled || last.PostHooks.Terminal.Code != domain.HookCancelled {
		t.Fatal("terminal cancellation rewrote original conflict")
	}
	fmt.Fprintln(os.Stdout, "HOOK_FAILURE_FACTS=conflict/failure/cancelled")
}

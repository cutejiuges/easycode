//go:build darwin

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
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/tool"
)

// TestInputRewriteTerminalHelper 仅由真实 PTY 验收启用，普通测试不打开终端。
func TestInputRewriteTerminalHelper(t *testing.T) {
	name := os.Getenv("EASYCODE_REWRITE_TERMINAL_PROVIDER")
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
	workspace := t.TempDir()
	t.Chdir(workspace)
	content := "界\nline\x1b[31m\n"
	input, _ := domain.NewHookWriteInput("terminal-effective", content)
	writeInputRewriteConfig(t, workspace, input, false)
	path := filepath.Join(workspace, ".easycode", "hooks.json")
	b := readAppTestFile(t, path)
	var config struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					ID      string `json:"id"`
					Type    string `json:"type"`
					Command string `json:"command"`
					Timeout int    `json:"timeout"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	hook := &config.Hooks.PreToolUse[0].Hooks[0]
	hook.Command = `payload=$(cat); case "$payload" in *cancel*) /bin/sleep 30 ;; *) ` + strings.TrimPrefix(hook.Command, "cat >/dev/null; ") + ` ;; esac`
	hook.Timeout = 30
	b, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, path, b, 0600)
	fmt.Fprintf(os.Stdout, "REWRITE_WORKSPACE=%s\n", workspace)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		var calls []writeScenarioCall
		if n%2 == 1 {
			content := "original"
			if n == 7 {
				content = "cancel"
			}
			calls = []writeScenarioCall{{"Write", fmt.Sprintf(`{"file_path":"terminal-original","content":%q}`, content)}}
		}
		serveWriteScenarioSample(w, family, int(n), calls)
	}))
	defer server.Close()
	root := filepath.Join(privateAppTempDir(t), "sessions")
	result := Run(context.Background(), Options{Mode: headless.ModeInteractive, ConfigPath: writeAppConfig(t, t.TempDir(), family, server.URL), SessionDataRoot: root, Input: os.Stdin, Output: os.Stdout})
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
			first := bytes.SplitN(b, []byte("\n"), 2)[0]
			var envelope struct {
				Thread domain.ThreadID `json:"thread_id"`
			}
			if err := json.Unmarshal(first, &envelope); err != nil {
				return err
			}
			thread = envelope.Thread
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, plan := loadToolLoopPlan(t, root, thread)
	if len(plan.Tools) != 4 {
		t.Fatal("terminal lost rewrite calls")
	}
	if plan.Tools[0].Result.Code() != string(domain.HookTrustDenied) || plan.Tools[1].Result.Status() != tool.ResultDenied || plan.Tools[1].Admission.Sequence() != 0 || !strings.Contains(plan.Tools[1].Result.Preview().Text(), "tool was not executed") {
		t.Fatal("default denials admitted effects")
	}
	if plan.Tools[2].Result.Status() != tool.ResultSuccess || plan.Tools[2].Admission.Sequence() == 0 || !strings.Contains(plan.Tools[2].Result.Preview().Text(), "Admitted tool attempt") || plan.Tools[3].Result.Status() != tool.ResultCancelled {
		t.Fatal("terminal changed execution or cancellation facts")
	}
	if b, err := os.ReadFile(filepath.Join(workspace, "terminal-effective")); err != nil || string(b) != content {
		t.Fatal("terminal effective content changed", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "terminal-original")); !os.IsNotExist(err) {
		t.Fatal("original terminal target executed")
	}
	fmt.Fprintf(os.Stdout, "\nREWRITE_TERMINAL_FACTS=denied/denied/success/cancelled\nREWRITE_TERMINAL_EXIT=%d REQUESTS=%d\n", result.ExitCode(), requests.Load())
}

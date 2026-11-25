//go:build darwin || linux

package app

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/session"
)

func TestTaskBuiltCLICompactionQueriesAndIndependentResumeRequests(t *testing.T) {
	binary := buildAppCLI(t)
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			var mu sync.Mutex
			var requests [][]byte
			summaries := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if bytes.Contains(body, []byte("session-start-cli-sentinel")) {
					t.Error("headless sent SessionStart output to the Provider")
				}
				if err := validateTaskServiceSchema(family, body); err != nil {
					t.Error("real CLI request has an invalid task schema", err)
					http.Error(w, "invalid task schema", http.StatusBadRequest)
					return
				}
				isSummary := bytes.Contains(body, []byte(`"tool_choice":"none"`)) || bytes.Contains(body, []byte(`"tool_choice":{"type":"none"}`))
				mu.Lock()
				requests = append(requests, body)
				n := len(requests)
				if isSummary {
					summaries++
				}
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				if isSummary {
					var wire bytes.Buffer
					serveWriteScenarioSample(&wire, family, n, nil)
					data := wire.Bytes()
					if family == domain.ProviderAnthropic {
						data = bytes.Replace(data, []byte(`data: {"type":"message_stop"}`), []byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\ndata: {\"type\":\"message_stop\"}"), 1)
					}
					_, _ = w.Write(data)
					return
				}
				var calls []writeScenarioCall
				if n == 1 {
					calls = []writeScenarioCall{taskScenarioCall(family, false)}
				}
				serveWriteScenarioSample(w, family, n, calls)
			}))
			defer server.Close()
			configuration := writeAppConfig(t, privateAppTempDir(t), family, server.URL)
			home, workspace := privateAppTempDir(t), privateAppTempDir(t)
			hookDir := filepath.Join(workspace, ".easycode")
			if err := os.Mkdir(hookDir, 0700); err != nil {
				t.Fatal(err)
			}
			const hooks = `{"hooks":{"PostCompact":[{"matcher":"manual","hooks":[{"id":"check-summary","type":"command","command":"cat >/dev/null; printf post-compact-cli-sentinel; exit 97"}]}],"SessionStart":[{"matcher":"*","hooks":[{"id":"start-context","type":"command","feedback":"model","command":"printf '%s' '{\"hookSpecificOutput\":{\"hookEventName\":\"SessionStart\",\"additionalContext\":\"session-start-cli-sentinel\"}}'"}]}]}}`
			writeAppTestFile(t, filepath.Join(hookDir, "hooks.json"), []byte(hooks), 0600)
			requestCount := func() int { mu.Lock(); defer mu.Unlock(); return len(requests) }
			thread, prefix, saved := runTaskCompactionCLI(t, binary, home, workspace, configuration, requestCount)
			loaded := loadHeadlessJournal(t, filepath.Join(home, ".easycode", "sessions"), thread)
			assertNoSessionStartFrames(t, loaded, "", "")
			plan, err := session.NewReplayPlanner().Plan(loaded)
			if err != nil || len(plan.Compactions) != 1 || plan.Compactions[0].Completed == nil {
				t.Fatal("headless compaction did not save a replayable checkpoint", err)
			}
			post := plan.Compactions[0].Completed.PostCompact
			if post.Kind() != session.PostCompactPlanSkipped || post.Code() != domain.HookTrustRequired {
				t.Fatal("matched headless observer was not skipped for lack of interactive trust", post.Kind(), post.Code())
			}
			mu.Lock()
			count, summaryCount := len(requests), summaries
			var uninterrupted []byte
			if count >= 4 {
				uninterrupted = bytes.Clone(requests[3])
			}
			mu.Unlock()
			if count != 4 || summaryCount != 1 {
				t.Fatal("live CLI did not execute one task and one summary", count, summaryCount)
			}
			occurrences := 1
			if family == domain.ProviderAnthropic {
				// 此输入的 activeForm 也包含标签，两个规范字段应各保存一次。
				occurrences = 2
			}
			if bytes.Contains(uninterrupted, []byte("task-compaction-archived-history")) || strings.Count(string(uninterrupted), "检查中文代码") != occurrences || bytes.Contains(uninterrupted, []byte("result_sequence")) || bytes.Contains(uninterrupted, []byte(`"profile"`)) || bytes.Contains(uninterrupted, []byte("session-start-cli-sentinel")) {
				t.Fatal("compacted continuation duplicated tasks, retained archived input or disclosed provenance")
			}
			modes := []string{"stream-json", "print"}
			if family == domain.ProviderAnthropic {
				modes = []string{"stream-json", "json"}
			}
			for _, mode := range modes {
				t.Run("resume_"+mode, func(t *testing.T) {
					resumedHome := privateAppTempDir(t)
					installTaskCLIPrefix(t, resumedHome, thread, prefix)
					args := []string{"--config", configuration, "--resume", string(thread)}
					input := ""
					if mode == "print" {
						args = append(args, "--print", "same continuation after checkpoint")
					} else if mode == "json" {
						args = append(args, "--json", "same continuation after checkpoint")
					} else {
						args = append(args, "--json", "--input-format", "stream-json")
						input = "{\"version\":1,\"type\":\"tasks.query\",\"request_id\":\"restored-tasks\"}\n{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"continue\",\"text\":\"same continuation after checkpoint\"}\n"
					}
					before := requestCount()
					result := runTaskCLI(t, binary, resumedHome, workspace, input, args...)
					if result.exit != 0 || !strings.Contains(result.stdout, "answer-") || strings.Contains(result.stdout+result.stderr, "file-secret") {
						t.Fatal("independent compacted CLI resume failed", result)
					}
					mu.Lock()
					count, summaryCount := len(requests), summaries
					canonical := bytes.Clone(requests[len(requests)-1])
					mu.Unlock()
					if count != before+1 || summaryCount != 1 || !bytes.Equal(uninterrupted, canonical) {
						t.Fatal("independent restored CLI changed full canonical request or repeated task/summary")
					}
					if mode == "stream-json" {
						response := strings.Index(result.stdout, `"command_type":"tasks.query"`)
						snapshot := strings.Index(result.stdout, `"type":"tasks.snapshot"`)
						if response < 0 || snapshot <= response || strings.Count(result.stdout, `"type":"tasks.snapshot"`) != 1 {
							t.Fatal("restored query response/snapshot order changed")
						}
						for _, line := range strings.Split(result.stdout, "\n") {
							if !strings.Contains(line, `"type":"tasks.snapshot"`) {
								continue
							}
							value, err := headless.DecodeTaskSnapshotEvent([]byte(line))
							if err != nil || !value.Snapshot.Progress.Equal(saved) {
								t.Fatal("independent resume lost exact task source", err)
							}
						}
					} else if strings.Contains(result.stdout, "tasks.snapshot") || strings.Contains(result.stdout, "检查中文代码") {
						t.Fatal("one-shot resume emitted task frames or echoed task text")
					}
					loaded := loadHeadlessJournal(t, filepath.Join(resumedHome, ".easycode", "sessions"), thread)
					assertNoSessionStartFrames(t, loaded, result.stdout, result.stderr)
					plan, err := session.NewReplayPlanner().Plan(loaded)
					state, stateErr := plan.TaskProgress()
					if err != nil || stateErr != nil || !state.Equal(saved) || len(plan.Compactions) != 1 || len(plan.Tools) != 1 {
						t.Fatal("resume rewrote task truth or repeated compaction", err, stateErr)
					}
				})
			}
		})
	}
}

func assertNoSessionStartFrames(t *testing.T, loaded session.LoadResult, stdout, stderr string) {
	t.Helper()
	for _, record := range loaded.Records {
		switch record.EventKind {
		case session.EventSessionStartHookPlanned, session.EventSessionStartHookStarted, session.EventSessionStartHookFinished, session.EventSessionStartHookClosed:
			t.Fatalf("headless saved a SessionStart fact: %s", record.EventKind)
		}
	}
	if strings.Contains(stdout, "session_start") || strings.Contains(stdout, "session.start") ||
		strings.Contains(stdout+stderr, "session-start-cli-sentinel") {
		t.Fatal("headless emitted SessionStart frames or command output")
	}
}

func installTaskCLIPrefix(t *testing.T, home string, thread domain.ThreadID, prefix []byte) {
	t.Helper()
	repo, err := session.OpenOrCreateRepository(filepath.Join(home, ".easycode", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Create(context.Background(), thread)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	path, _ := repo.JournalPath(thread)
	writeAppTestFile(t, path, prefix, 0600)
}

// runTaskCompactionCLI 由真实 stdout 终态驱动 stdin，返回 checkpoint 时的不可变历史前缀。
func runTaskCompactionCLI(t *testing.T, binary, home, workspace, configuration string, requests func() int) (domain.ThreadID, []byte, domain.TaskProgressState) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--config", configuration, "--json", "--input-format", "stream-json")
	cmd.Dir = workspace
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "HOME" && !strings.HasPrefix(key, "EASYCODE_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+home)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 128<<10)
	var thread domain.ThreadID
	var compactionFrames []string
	var observeCompactionFrames bool
	readUntil := func(want string) []byte {
		t.Helper()
		for scanner.Scan() {
			line := bytes.Clone(scanner.Bytes())
			var event struct {
				Type   string          `json:"type"`
				Thread domain.ThreadID `json:"thread_id"`
			}
			if codec.Unmarshal(line, &event) != nil {
				t.Fatal("invalid CLI JSONL")
			}
			if strings.Contains(event.Type, "hook") || strings.Contains(event.Type, "session_start") || strings.Contains(event.Type, "session.start") ||
				bytes.Contains(line, []byte("post-compact-cli-sentinel")) || bytes.Contains(line, []byte("session-start-cli-sentinel")) {
				t.Fatal("CLI emitted local hook inspection data", string(line))
			}
			if observeCompactionFrames {
				compactionFrames = append(compactionFrames, event.Type)
			}
			if event.Thread.Valid() {
				thread = event.Thread
			}
			if event.Type == "turn.failed" || event.Type == "compaction.failed" || event.Type == "error" {
				t.Fatal("CLI operation failed", string(line), stderr.String())
			}
			if event.Type == want {
				return line
			}
		}
		t.Fatal("missing CLI event", want, scanner.Err(), stderr.String())
		return nil
	}
	text := "task-compaction-archived-history " + strings.Repeat("historical work ", 3500)
	_, _ = fmt.Fprintf(stdin, "{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"first\",\"text\":%q}\n", text)
	readUntil("turn.completed")
	repo, err := session.OpenOrCreateRepository(filepath.Join(home, ".easycode", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	path, err := repo.JournalPath(thread)
	_ = repo.Close()
	if err != nil {
		t.Fatal(err)
	}
	query := func(id string) domain.TaskProgressState {
		t.Helper()
		before := readAppTestFile(t, path)
		network := requests()
		_, _ = fmt.Fprintf(stdin, "{\"version\":1,\"type\":\"tasks.query\",\"request_id\":%q}\n", id)
		response := readUntil("control.response")
		if !bytes.Contains(response, []byte(`"request_id":"`+id+`"`)) || !bytes.Contains(response, []byte(`"disposition":"queried"`)) {
			t.Fatal("task query response was reordered")
		}
		value, err := headless.DecodeTaskSnapshotEvent(readUntil("tasks.snapshot"))
		if err != nil || string(value.RequestID) != id || value.ThreadID != thread {
			t.Fatal("task query snapshot identity changed", err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) || requests() != network {
			t.Fatal("task query mutated native history or contacted the model")
		}
		return value.Snapshot.Progress
	}
	saved := query("before-compact")
	if saved.Kind() != domain.TaskProgressSnapshot || saved.Counts().Total != 2 {
		t.Fatal("task creation was not durable before CLI compaction")
	}
	observeCompactionFrames = true
	_, _ = io.WriteString(stdin, "{\"version\":1,\"type\":\"context.compact\",\"request_id\":\"compact\",\"instructions\":\"Preserve the current task list\"}\n")
	response := readUntil("control.response")
	if !bytes.Contains(response, []byte(`"request_id":"compact"`)) || !bytes.Contains(response, []byte(`"disposition":"compacting"`)) {
		t.Fatal("CLI compaction admission failed", string(response))
	}
	readUntil("compaction.started")
	completed := readUntil("compaction.completed")
	observeCompactionFrames = false
	if !slices.Equal(compactionFrames, []string{"control.response", "compaction.started", "compaction.completed"}) ||
		bytes.Contains(completed, []byte("post_compact")) || bytes.Contains(completed, []byte("session_start")) || bytes.Contains(completed, []byte(domain.HookTrustRequired)) {
		t.Fatal("matching hooks changed the public stream v1 compaction frames", compactionFrames, string(completed))
	}
	if !query("after-compact").Equal(saved) {
		t.Fatal("CLI compaction changed the task source or content")
	}
	prefix := readAppTestFile(t, path)
	_, _ = io.WriteString(stdin, "{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"after\",\"text\":\"same continuation after checkpoint\"}\n")
	readUntil("turn.completed")
	_ = stdin.Close()
	for scanner.Scan() {
		if bytes.Contains(scanner.Bytes(), []byte(`"type":"tasks.snapshot"`)) {
			t.Fatal("EOF emitted an unsolicited snapshot")
		}
		if bytes.Contains(scanner.Bytes(), []byte("session_start")) || bytes.Contains(scanner.Bytes(), []byte("session.start")) ||
			bytes.Contains(scanner.Bytes(), []byte("session-start-cli-sentinel")) {
			t.Fatal("EOF emitted SessionStart data")
		}
	}
	err = cmd.Wait()
	waited = true
	if err != nil || ctx.Err() != nil {
		t.Fatal("compacted CLI did not drain or release its resources", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "session-start-cli-sentinel") {
		t.Fatal("CLI exposed SessionStart output on stderr")
	}
	return thread, prefix, saved
}

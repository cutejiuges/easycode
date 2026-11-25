//go:build darwin

package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

type preCompactTerminalModel struct {
	inner tea.Model
	ack   *os.File
}

func (m preCompactTerminalModel) Init() tea.Cmd { return m.inner.Init() }
func (m preCompactTerminalModel) View() string  { return m.inner.View() }
func (m preCompactTerminalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, keyed := msg.(tea.KeyMsg)
	if keyed && key.Type == tea.KeyF10 {
		return m, tea.Quit
	}
	before := m.inner.View()
	next, command := m.inner.Update(msg)
	m.inner = next
	ack := taskTerminalAck{View: m.inner.View()}
	switch value := msg.(type) {
	case tea.KeyMsg:
		ack.Kind = "key"
	case tea.WindowSizeMsg:
		ack.Kind, ack.Width, ack.Height = "size", value.Width, value.Height
	case protocol.ControlItem:
		event, ok := value.ContextEvent()
		if !ok {
			event, ok = value.TurnEvent()
		}
		if !ok {
			break
		}
		switch event.Kind {
		case protocol.EventCompactionStarted, protocol.EventHookTrustRequested, protocol.EventHookTrustResolved,
			protocol.EventCompactionSummarizing, protocol.EventCompactionCompleted, protocol.EventTurnFailed,
			protocol.EventTurnCompleted, protocol.EventPostCompactInstalled:
			ack.Kind = string(event.Kind)
		case protocol.EventPostCompactClosed:
			closed, err := protocol.DecodePostCompactClosed(event)
			if err == nil {
				ack.Kind, ack.State = string(event.Kind), string(closed.Code)
			}
		case protocol.EventCompactionFailed:
			failed, err := protocol.DecodeCompactionFailed(event)
			if err == nil {
				ack.Kind, ack.State = string(event.Kind), string(failed.Code)
			}
		case protocol.EventHookActivity:
			activity, err := protocol.DecodeHookActivity(event)
			if err == nil && (activity.Reference.Event == domain.HookPreCompact || activity.Reference.Event == domain.HookPostCompact) && activity.State == protocol.HookActivityChecking {
				ack.Kind = "checking"
			}
		}
	}
	if ack.Kind != "" {
		writeTaskTerminalAck(m.ack, ack)
	} else if ack.View != before {
		ack.Kind = "view"
		writeTaskTerminalAck(m.ack, ack)
	}
	return m, command
}

// TestPreCompactTerminalHelper 由真实 PTY 脚本启动，Provider 请求只访问本地模拟端点。
func TestPreCompactTerminalHelper(t *testing.T) {
	mode := os.Getenv("EASYCODE_PRE_COMPACT_TERMINAL")
	if mode == "" {
		return
	}
	post := mode == "post"
	family := domain.ProviderFamily(os.Getenv("EASYCODE_PRE_COMPACT_FAMILY"))
	if !family.Valid() || mode != "manual" && mode != "auto" && !post {
		t.Fatal("invalid compact terminal fixture")
	}
	ack := openTerminalAck(t, "EASYCODE_PRE_COMPACT_ACK_FD", "pre-compact-ack")
	defer ack.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	clearProviderEnvironment(t)
	t.Setenv("HOME", privateAppTempDir(t))
	workspace := privateAppTempDir(t)
	root := filepath.Join(privateAppTempDir(t), "sessions")
	var requests, summaries atomic.Int32
	const postOutput = "terminal-post-compact-private-output"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := requests.Add(1)
		isSummary := bytes.Contains(body, []byte(`"tool_choice":"none"`)) || bytes.Contains(body, []byte(`"tool_choice":{"type":"none"}`))
		text := fmt.Sprintf("terminal-answer-%d", n)
		if isSummary {
			summaries.Add(1)
			if !post && !bytes.Contains(body, []byte(`terminal-user-focus\n\nterminal-hook-focus`)) {
				t.Error("summary did not receive ordered focus")
			}
			text = "terminal compacted summary"
		}
		if post && bytes.Contains(body, []byte(postOutput)) {
			t.Error("post hook output reached a provider request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		serveStopAssistant(w, family, text)
	}))
	defer server.Close()
	cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("pre-compact-terminal-fixture")}}
	if mode == "auto" {
		var err error
		cfg.ContextBudget, err = contextplan.NewBudget(22000, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed := mustOpenChatResources(t, ctx, cfg, root, "", workspace)
	defer seed.close(context.Background())
	text := strings.Repeat("historical work ", 1000)
	if mode == "auto" {
		text = strings.Repeat("historical work ", 3500)
	}
	submitAppTurn(t, seed, text)
	thread := seed.identity.ThreadID
	if err := seed.close(ctx); err != nil {
		t.Fatal(err)
	}
	hookDirectory := filepath.Join(workspace, ".easycode")
	if err := os.MkdirAll(hookDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	event, matcher, scriptName := domain.HookPreCompact, mode, "focus-check.sh"
	declaration := map[string]string{"id": "retain-verification", "type": "command", "command": "sh .easycode/" + scriptName, "feedback": "model"}
	script := `case "$(cat)" in *terminal-user-focus*) printf '%s' '{"hookSpecificOutput":{"hookEventName":"PreCompact","summaryInstructions":"terminal-hook-focus"}}' ;; *) exit 1 ;; esac`
	if post {
		event, matcher, scriptName = domain.HookPostCompact, "manual", "observe-check.sh"
		declaration = map[string]string{"id": "observe-summary", "type": "command", "command": "sh .easycode/" + scriptName}
		private := filepath.Join(privateAppTempDir(t), "private-file")
		writeAppTestFile(t, private, []byte("private"), 0600)
		t.Setenv("EASYCODE_POST_COMPACT_SECRET", "private")
		port := server.Listener.Addr().(*net.TCPAddr).Port
		script = fmt.Sprintf(`input=$(/bin/cat) || exit 10
case "$input" in *'"hook_event_name":"PostCompact"'*'"trigger":"manual","compact_summary":"terminal compacted summary"'*) ;; *) exit 11 ;; esac
test -z "$EASYCODE_POST_COMPACT_SECRET" && test -r .easycode/observe-check.sh || exit 12
(printf forbidden > post-compact-forbidden) 2>/dev/null && exit 13
/bin/cat %q >/dev/null 2>&1 && exit 14
/usr/bin/nc -G 1 -z 127.0.0.1 %d >/dev/null 2>&1 && exit 15
printf '%%s' %q
printf '%%s' %q >&2
exit 2`, private, port, postOutput, postOutput+"-stderr")
	}
	hookConfig, err := codec.MarshalStable(map[string]map[domain.HookEvent][]map[string]any{"hooks": {event: {{
		"matcher": matcher, "hooks": []map[string]string{declaration},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, filepath.Join(hookDirectory, "hooks.json"), hookConfig, 0600)
	writeAppTestFile(t, filepath.Join(hookDirectory, scriptName), []byte(script), 0600)
	resources := mustOpenChatResourcesWithHooks(t, ctx, cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{interactiveApprovals: true})
	defer resources.close(context.Background())
	model, err := assembleChatModel(ctx, resources)
	if err != nil {
		t.Fatal(err)
	}
	program := tea.NewProgram(preCompactTerminalModel{model, ack}, tea.WithContext(ctx), tea.WithoutSignalHandler())
	writeTaskTerminalAck(ack, taskTerminalAck{Kind: "ready"})
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	if err := resources.close(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, replay := loadToolLoopPlan(t, root, thread)
	if post {
		if requests.Load() != 5 || summaries.Load() != 2 {
			t.Fatalf("unexpected provider requests: %d requests, %d summaries", requests.Load(), summaries.Load())
		}
		if _, err := os.Stat(filepath.Join(workspace, "post-compact-forbidden")); !os.IsNotExist(err) {
			t.Fatal("post hook wrote to the workspace")
		}
		assertPostCompactTerminalFacts(t, loaded, replay, postOutput)
	} else if mode == "auto" {
		if requests.Load() != 1 || summaries.Load() != 0 || len(replay.Compactions) != 1 || replay.Compactions[0].Failed == nil || replay.Compactions[0].Failed.Code != protocol.CompactionFailureCancelled {
			t.Fatal("automatic review cancellation emitted an unexpected request or terminal")
		}
	} else if requests.Load() != 2 || summaries.Load() != 1 || len(replay.Compactions) != 3 ||
		replay.Compactions[0].Failed == nil || replay.Compactions[0].Failed.Code != protocol.CompactionFailureCancelled ||
		replay.Compactions[1].Failed == nil || replay.Compactions[1].Failed.Code != protocol.CompactionFailureHookTrustDenied || replay.Compactions[2].Completed == nil {
		t.Fatal("manual review cancellation, refusal and summarization did not close independently")
	}
	writeTaskTerminalAck(ack, taskTerminalAck{Kind: "total", Number: int(requests.Load())})
}

func assertPostCompactTerminalFacts(t *testing.T, loaded session.LoadResult, replay session.ReplayPlan, output string) {
	t.Helper()
	if len(replay.Compactions) != 2 || replay.Compactions[0].Completed == nil || replay.Compactions[1].Completed == nil || replay.InterruptedPostCompact != nil {
		t.Fatal("denied and failed post checks must each keep their checkpoint")
	}
	var started, finished int
	var closed []session.PostCompactHookClosedPayload
	for _, record := range loaded.Records {
		if bytes.Contains(record.Payload, []byte(output)) {
			t.Fatal("post hook output reached the journal")
		}
		switch record.EventKind {
		case session.EventPostCompactHookStarted:
			started++
		case session.EventPostCompactHookFinished:
			finished++
			result, err := session.DecodePostCompactHookFinishedPayload(record)
			id, _, _ := result.Identity.Target.Compaction()
			if err != nil || id != replay.Compactions[1].Started.ID || result.Code != domain.HookExitFailed ||
				!result.ExitKnown || result.ExitCode != 2 || !result.CleanupConfirmed || result.StdoutBytes == 0 || result.StderrBytes == 0 {
				t.Fatalf("trusted sandbox probe failed unexpectedly: %+v, %v", result, err)
			}
		case session.EventPostCompactHookClosed:
			result, err := session.DecodePostCompactHookClosedPayload(record)
			if err != nil {
				t.Fatal(err)
			}
			closed = append(closed, result)
		}
	}
	if started != 1 || finished != 1 || len(closed) != 2 || closed[0].State != protocol.PostCompactSkipped || closed[0].Code != domain.HookTrustDenied ||
		closed[0].Finished != 0 || closed[1].State != protocol.PostCompactFailed || closed[1].Code != domain.HookExitFailed || closed[1].Finished != 1 {
		t.Fatalf("incorrect check closures: %d starts, %d finishes, %+v", started, finished, closed)
	}
}

//go:build darwin

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
	"strconv"
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
			protocol.EventCompactionSummarizing, protocol.EventCompactionCompleted, protocol.EventTurnFailed:
			ack.Kind = string(event.Kind)
		case protocol.EventCompactionFailed:
			failed, err := protocol.DecodeCompactionFailed(event)
			if err == nil {
				ack.Kind, ack.State = string(event.Kind), string(failed.Code)
			}
		case protocol.EventHookActivity:
			activity, err := protocol.DecodeHookActivity(event)
			if err == nil && activity.Reference.Event == domain.HookPreCompact && activity.State == protocol.HookActivityChecking {
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
	family := domain.ProviderFamily(os.Getenv("EASYCODE_PRE_COMPACT_FAMILY"))
	if !family.Valid() || mode != "manual" && mode != "auto" {
		t.Fatal("invalid compact terminal fixture")
	}
	fd, err := strconv.Atoi(os.Getenv("EASYCODE_PRE_COMPACT_ACK_FD"))
	if err != nil {
		t.Fatal(err)
	}
	ack := os.NewFile(uintptr(fd), "pre-compact-ack")
	defer ack.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	clearProviderEnvironment(t)
	t.Setenv("HOME", privateAppTempDir(t))
	workspace := privateAppTempDir(t)
	root := filepath.Join(privateAppTempDir(t), "sessions")
	var requests, summaries atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := requests.Add(1)
		isSummary := bytes.Contains(body, []byte(`"tool_choice":"none"`)) || bytes.Contains(body, []byte(`"tool_choice":{"type":"none"}`))
		text := fmt.Sprintf("terminal-answer-%d", n)
		if isSummary {
			summaries.Add(1)
			if !bytes.Contains(body, []byte(`terminal-user-focus\n\nterminal-hook-focus`)) {
				t.Error("summary did not receive ordered focus")
			}
			text = "terminal compacted summary"
		}
		w.Header().Set("Content-Type", "text/event-stream")
		serveStopAssistant(w, family, text)
	}))
	defer server.Close()
	cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("pre-compact-terminal-fixture")}}
	if mode == "auto" {
		cfg.ContextBudget, err = contextplan.NewBudget(22000, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed, err := openChatResources(ctx, cfg, root, "", workspace)
	if err != nil {
		t.Fatal(err)
	}
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
	hookConfig, err := codec.MarshalStable(map[string]map[string][]map[string]any{"hooks": {"PreCompact": {{
		"matcher": mode, "hooks": []map[string]string{{"id": "retain-verification", "type": "command", "command": "sh .easycode/focus-check.sh", "feedback": "model"}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDirectory, "hooks.json"), hookConfig, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hookDirectory, "focus-check.sh"), []byte(`case "$(cat)" in *terminal-user-focus*) printf '%s' '{"hookSpecificOutput":{"hookEventName":"PreCompact","summaryInstructions":"terminal-hook-focus"}}' ;; *) exit 1 ;; esac`), 0600); err != nil {
		t.Fatal(err)
	}
	resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{interactiveApprovals: true})
	if err != nil {
		t.Fatal(err)
	}
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
	_, replay := loadToolLoopPlan(t, root, thread)
	if mode == "auto" {
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

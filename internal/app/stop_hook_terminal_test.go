package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"

	tea "github.com/charmbracelet/bubbletea"
)

// stopTerminalAck 仅供 PTY 握手，关联真实 turn 与已保存的检查阶段。
type stopTerminalAck struct {
	Event       protocol.EventKind     `json:"event"`
	TurnID      domain.TurnID          `json:"turn_id"`
	Phase       protocol.StopHookPhase `json:"phase,omitempty"`
	SampleIndex uint32                 `json:"sample_index"`
	Code        string                 `json:"code,omitempty"`
}

type stopTerminalModel struct {
	model tea.Model
	ack   *json.Encoder
}

func (m stopTerminalModel) Init() tea.Cmd { return m.model.Init() }
func (m stopTerminalModel) View() string  { return m.model.View() }
func (m stopTerminalModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(message)
	m.model = next
	item, ok := message.(protocol.ControlItem)
	if !ok {
		return m, cmd
	}
	event, ok := item.TurnEvent()
	if !ok {
		return m, cmd
	}
	ack := stopTerminalAck{Event: event.Kind, TurnID: event.TurnID}
	switch event.Kind {
	case protocol.EventTurnStarted, protocol.EventTurnCompleted:
	case protocol.EventTurnFailed:
		p, err := protocol.DecodeTurnFailed(event)
		if err != nil {
			return m, cmd
		}
		ack.Code = p.Code
	case protocol.EventStopHookCommitted:
		p, err := protocol.DecodeStopHookCommitted(event)
		if err != nil {
			return m, cmd
		}
		sample, ok := p.Target.AssistantSample()
		if !ok {
			return m, cmd
		}
		ack.Phase, ack.SampleIndex = p.Phase, sample.SampleIndex
	default:
		return m, cmd
	}
	_ = m.ack.Encode(ack)
	return m, cmd
}

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
	fd, err := strconv.Atoi(os.Getenv("EASYCODE_STOP_TERMINAL_ACK_FD"))
	if err != nil {
		t.Fatal(err)
	}
	ack := os.NewFile(uintptr(fd), "stop-terminal-ack")
	defer ack.Close()
	clearProviderEnvironment(t)
	t.Setenv("HOME", privateAppTempDir(t))
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
	cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")}}
	root := filepath.Join(privateAppTempDir(t), "sessions")
	resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	model, err := assembleChatModel(context.Background(), resources)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tea.NewProgram(stopTerminalModel{model: model, ack: json.NewEncoder(ack)}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run(); err != nil {
		t.Fatal(err)
	}
	thread := resources.identity.ThreadID
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, plan := loadToolLoopPlan(t, root, thread)
	if calls.Load() != 4 || len(plan.Stops) != 4 || plan.StopRecovery != nil {
		t.Fatal("unexpected terminal result")
	}
	last := plan.Stops[len(plan.Stops)-1]
	if last.Decision == nil || last.Decision.Disposition != domain.StopCancelled || last.Decision.Code != domain.HookCancelled || last.Decision.Processed != 1 || len(last.Finished) != 1 || last.Finished[0].Result.Status != domain.HookCancel {
		t.Fatal("terminal cancelled before the intended completion check")
	}
	fmt.Fprintln(os.Stdout, "STOP_CANCEL_FACTS=cancelled/started")
	fmt.Fprintf(os.Stdout, "STOP_TERMINAL_EXIT=0 REQUESTS=%d\n", calls.Load())
}

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	tea "github.com/charmbracelet/bubbletea"
)

type completionTerminalAck struct {
	Kind   string `json:"kind"`
	Key    string `json:"key"`
	View   string `json:"view"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Event  string `json:"event,omitempty"`
}

type completionTerminalModel struct {
	model tea.Model
	ack   *json.Encoder
}

func (m completionTerminalModel) Init() tea.Cmd {
	_ = m.ack.Encode(completionTerminalAck{Kind: "ready", View: m.model.View()})
	return m.model.Init()
}
func (m completionTerminalModel) View() string { return m.model.View() }
func (m completionTerminalModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(message)
	m.model = next
	ack := completionTerminalAck{View: m.model.View()}
	switch value := message.(type) {
	case tea.KeyMsg:
		ack.Kind = "key"
		ack.Key = value.String()
	case tea.WindowSizeMsg:
		ack.Kind = "size"
		ack.Width = value.Width
		ack.Height = value.Height
	case protocol.ControlItem:
		ack.Kind = "control"
		if event, ok := value.TurnEvent(); ok {
			ack.Event = string(event.Kind)
			if event.Kind == protocol.EventTurnCompleted {
				ack.Kind = "idle"
			}
		}
	}
	if ack.Kind != "" {
		_ = m.ack.Encode(ack)
	}
	return m, cmd
}

// TestCompletionTerminalHelper 仅供显式 PTY 验收使用；普通测试不接管终端。
func TestCompletionTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_COMPLETION_TERMINAL_SMOKE") != "1" {
		return
	}
	t.Setenv("HOME", t.TempDir())
	fd, err := strconv.Atoi(os.Getenv("EASYCODE_COMPLETION_ACK_FD"))
	if err != nil {
		t.Fatal(err)
	}
	ack := os.NewFile(uintptr(fd), "completion-ack")
	defer ack.Close()
	workspace := privateAppTempDir(t)
	writeCompletionSkill(t, workspace, false, "PTY Skill $ARGUMENTS")
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: os.Getenv("EASYCODE_COMPLETION_FIXTURE_URL"), APIKey: secret.New("fixture-key"), Model: "fixture-model"}}
	res, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: filepath.Join(privateAppTempDir(t), "sessions")}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
	if err != nil {
		t.Fatal(err)
	}
	defer res.close(context.Background())
	model, err := assembleChatModel(context.Background(), res)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tea.NewProgram(completionTerminalModel{model: model, ack: json.NewEncoder(ack)}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run()
	if err != nil {
		t.Fatal(err)
	}
	if err := res.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "marker")); !os.IsNotExist(err) {
		t.Fatal("completion or approval keys executed Write")
	}
	fmt.Fprintln(os.Stdout, "COMPLETION_EXIT=0")
}

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	tea "github.com/charmbracelet/bubbletea"
)

type fileTerminalProbe struct{ kind string }
type fileTerminalAction struct{ err error }
type fileTerminalBoundary struct {
	workspaceFileBoundary
	reads, scans                   atomic.Int32
	discovery, preparation         chan struct{}
	discoveryOnce, preparationOnce sync.Once
	notify                         func(fileTerminalProbe)
}

func (f *fileTerminalBoundary) ReadWorkspaceFile(ctx context.Context, path string, limit int) (string, int, error) {
	text, count, err := f.workspaceFileBoundary.ReadWorkspaceFile(ctx, path, limit)
	if f.reads.Add(1) == 1 && err == nil {
		f.notify(fileTerminalProbe{"read_blocked"})
		select {
		case <-f.preparation:
		case <-ctx.Done():
			return "", count, ctx.Err()
		}
	}
	return text, count, err
}
func (f *fileTerminalBoundary) DiscoverFileNames(ctx context.Context, publish func([]string, domain.FileDiscoveryReport) (bool, error)) (domain.FileDiscoveryReport, error) {
	if f.scans.Add(1) != 1 {
		return f.workspaceFileBoundary.DiscoverFileNames(ctx, publish)
	}
	var paths []string
	report, err := f.workspaceFileBoundary.DiscoverFileNames(ctx, func(batch []string, _ domain.FileDiscoveryReport) (bool, error) {
		paths = append(paths, batch...)
		return true, nil
	})
	if err != nil {
		return report, err
	}
	if _, err := publish(paths, report); err != nil {
		return report, err
	}
	f.notify(fileTerminalProbe{"scan_blocked"})
	select {
	case <-f.discovery:
		return report, nil
	case <-ctx.Done():
		return report, ctx.Err()
	}
}

type fileTerminalAck struct {
	Kind          string `json:"kind"`
	Key           string `json:"key,omitempty"`
	View          string `json:"view"`
	Event         string `json:"event,omitempty"`
	Selected      string `json:"selected,omitempty"`
	Captured      string `json:"captured,omitempty"`
	Pending       bool   `json:"pending,omitempty"`
	Reads, Scans  int32
	Width, Height int
	Journal       string `json:"journal,omitempty"`
}
type fileTerminalModel struct {
	inner     tea.Model
	resources *chatResources
	boundary  *fileTerminalBoundary
	ack       *json.Encoder
	journal   string
}

func (m fileTerminalModel) View() string { return m.inner.View() }
func (m fileTerminalModel) Init() tea.Cmd {
	_ = m.ack.Encode(fileTerminalAck{Kind: "ready", View: m.inner.View(), Journal: m.journal})
	return m.inner.Init()
}
func (m fileTerminalModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	var command tea.Cmd
	if key, ok := message.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyF5:
			m.boundary.discoveryOnce.Do(func() { close(m.boundary.discovery) })
		case tea.KeyF7:
			m.boundary.preparationOnce.Do(func() { close(m.boundary.preparation) })
		case tea.KeyF6, tea.KeyF8:
			level := domain.EffortLevel("low")
			if key.Type == tea.KeyF8 {
				level = domain.EffortLevel("high")
			}
			choice, _ := domain.NewExplicitEffortChoice(level)
			command = func() tea.Msg {
				_, err := m.resources.session.SetEffort(context.Background(), protocol.RequestID("file-terminal-"+string(level)), choice)
				return fileTerminalAction{err}
			}
		case tea.KeyF10:
			command = tea.Quit
		}
	}
	next, cmd := m.inner.Update(message)
	m.inner = next
	ack := fileTerminalAck{Kind: "refresh", View: m.inner.View(), Reads: m.boundary.reads.Load(), Scans: m.boundary.scans.Load()}
	switch value := message.(type) {
	case tea.KeyMsg:
		ack.Kind, ack.Key = "key", value.String()
	case tea.WindowSizeMsg:
		ack.Kind, ack.Width, ack.Height = "size", value.Width, value.Height
	case fileTerminalProbe:
		ack.Kind = value.kind
	case fileTerminalAction:
		if value.err != nil {
			ack.Kind = "action_failed"
		}
	case protocol.ControlItem:
		ack.Kind = "control"
		if result, ok := value.CommandResult(); ok && result.Status() == protocol.CommandStatusAccepted {
			if result.CommandKind() == protocol.CommandSubmitConversationalInput {
				ack.Kind = "input_accepted"
			}
			if result.CommandKind() == protocol.CommandRetractInputs {
				ack.Kind = "retracted"
			}
		}
		if e, ok := value.TurnEvent(); ok {
			ack.Event = string(e.Kind)
			if e.Kind == protocol.EventUserInputCommitted {
				ack.Kind = "saved"
			}
			if e.Kind == protocol.EventTurnCompleted || e.Kind == protocol.EventTurnFailed {
				ack.Kind = "terminal"
			}
		}
		if e, ok := value.ContextEvent(); ok && e.Kind == protocol.EventEffortSnapshot {
			p, err := protocol.DecodeEffortSnapshot(e)
			if err == nil {
				ack.Kind = "effort"
				ack.Selected = string(p.Selected.Value)
				ack.Pending = p.Pending
				if p.Active != nil {
					ack.Captured = string(p.Active.Effort().Value)
				}
			}
		}
	}
	_ = m.ack.Encode(ack)
	return m, tea.Batch(cmd, command)
}

// TestFileMentionsTerminalHelper 仅由本地 PTY 脚本启动，生产模型和文件来源均按真实生命周期运行。
func TestFileMentionsTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_FILE_TERMINAL_SMOKE") != "1" {
		return
	}
	t.Setenv("HOME", privateAppTempDir(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ack := openTerminalAck(t, "EASYCODE_FILE_TERMINAL_ACK_FD", "file-terminal-ack")
	defer ack.Close()
	family := domain.ProviderFamily(os.Getenv("EASYCODE_FILE_TERMINAL_FAMILY"))
	cfg := reasoningFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: os.Getenv("EASYCODE_FILE_TERMINAL_URL"), APIKey: secret.New("fixture-key"), Model: "fixture-model"}})
	r := mustOpenChatResources(t, ctx, cfg, filepath.Join(privateAppTempDir(t), "sessions"), "", os.Getenv("EASYCODE_FILE_TERMINAL_WORKSPACE"))
	defer r.close(context.Background())
	f := &fileTerminalBoundary{workspaceFileBoundary: r.workspace, discovery: make(chan struct{}), preparation: make(chan struct{})}
	r.fileSource.workspace = f
	model, err := assembleChatModel(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	journal, _ := r.service.repository.JournalPath(r.identity.ThreadID)
	var program *tea.Program
	f.notify = func(value fileTerminalProbe) { program.Send(value) }
	program = tea.NewProgram(fileTerminalModel{inner: model, resources: r, boundary: f, ack: json.NewEncoder(ack), journal: journal}, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	cancel()
	closeAppTestResources(t, r)
	fmt.Fprintln(os.Stdout, "FILE_MENTIONS_EXIT=0")
}

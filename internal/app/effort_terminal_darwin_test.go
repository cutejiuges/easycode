//go:build darwin

package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type effortTerminalAck struct {
	Kind     string `json:"kind"`
	View     string `json:"view,omitempty"`
	Selected string `json:"selected,omitempty"`
	Captured string `json:"captured,omitempty"`
	Activity string `json:"activity,omitempty"`
	Phase    string `json:"phase,omitempty"`
	Revision uint64 `json:"revision"`
	Pending  bool   `json:"pending"`
	Number   int    `json:"number,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

func writeEffortTerminalAck(ack *os.File, value effortTerminalAck) {
	raw, err := codec.MarshalStable(value)
	if err == nil {
		_, _ = ack.Write(append(raw, '\n'))
	}
}

type effortTerminalModel struct {
	inner   tea.Model
	ack     *os.File
	release chan struct{}
	once    *sync.Once
}

func (m effortTerminalModel) Init() tea.Cmd { return m.inner.Init() }
func (m effortTerminalModel) View() string  { return m.inner.View() }
func (m effortTerminalModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := message.(tea.KeyMsg)
	if isKey && key.Type == tea.KeyF10 {
		return m, tea.Quit
	}
	if isKey && key.Type == tea.KeyF5 {
		m.once.Do(func() { close(m.release) })
	}
	next, cmd := m.inner.Update(message)
	m.inner = next
	if item, ok := message.(protocol.ControlItem); ok {
		if result, ok := item.CommandResult(); ok && result.CommandKind() == protocol.CommandSubmitConversationalInput && result.Status() == protocol.CommandStatusAccepted {
			writeEffortTerminalAck(m.ack, effortTerminalAck{Kind: "input_accepted", View: m.inner.View()})
		}
		if event, ok := item.ContextEvent(); ok {
			if event.Kind == protocol.EventEffortSnapshot {
				p, _ := protocol.DecodeEffortSnapshot(event)
				a := effortTerminalAck{Kind: "effort", View: m.inner.View(), Selected: string(p.Selected.Value), Activity: p.Activity, Revision: p.SelectionRevision, Pending: p.Pending}
				if p.Active != nil {
					a.Captured = string(p.Active.Effort().Value)
					a.Phase = p.Active.Phase()
				}
				writeEffortTerminalAck(m.ack, a)
			}
			if event.Kind == protocol.EventContextSnapshot {
				p, _ := protocol.DecodeContextSnapshot(event)
				a := effortTerminalAck{Kind: "context", View: m.inner.View(), Selected: string(p.Effort.Selected.Value), Activity: p.Activity, Pending: p.Effort.Pending}
				if p.Effort.Active != nil {
					a.Captured = string(p.Effort.Active.Effort().Value)
				}
				writeEffortTerminalAck(m.ack, a)
			}
		}
		if event, ok := item.TurnEvent(); ok {
			switch event.Kind {
			case protocol.EventAssistantReasoning:
				p, _ := protocol.DecodeAssistantReasoning(event)
				if p.Stage == protocol.ReasoningDelta {
					writeEffortTerminalAck(m.ack, effortTerminalAck{Kind: "live"})
				}
			case protocol.EventTurnCompleted:
				writeEffortTerminalAck(m.ack, effortTerminalAck{Kind: "complete"})
			case protocol.EventTurnFailed:
				writeEffortTerminalAck(m.ack, effortTerminalAck{Kind: "failed"})
			}
		}
	}
	if isKey {
		writeEffortTerminalAck(m.ack, effortTerminalAck{Kind: "key", View: m.inner.View()})
	}
	if size, ok := message.(tea.WindowSizeMsg); ok {
		writeEffortTerminalAck(m.ack, effortTerminalAck{Kind: "size", Width: size.Width, Height: size.Height, View: m.inner.View()})
	}
	return m, cmd
}

// TestEffortTerminalHelper 只由脚本启动，真实终端与本地服务均有明确完成信号。
func TestEffortTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_EFFORT_TERMINAL_SMOKE") != "1" {
		return
	}
	ack := openTerminalAck(t, "EASYCODE_EFFORT_ACK_FD", "effort-ack")
	defer ack.Close()
	family := domain.ProviderFamily(os.Getenv("EASYCODE_EFFORT_FAMILY"))
	if !family.Valid() {
		t.Fatal("invalid family")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	release := make(chan struct{})
	once := &sync.Once{}
	defer once.Do(func() { close(release) })
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := int(requests.Add(1))
		var wire struct {
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
			Output struct {
				Effort string `json:"effort"`
			} `json:"output_config"`
		}
		_ = codec.Unmarshal(body, &wire)
		selected := wire.Reasoning.Effort
		if family == domain.ProviderAnthropic {
			selected = wire.Output.Effort
		}
		writeEffortTerminalAck(ack, effortTerminalAck{Kind: "body", Selected: selected, Number: n})
		if bytes.Contains(body, []byte("same-name-effort-marker")) {
			writeEffortTerminalAck(ack, effortTerminalAck{Kind: "skill", Number: n})
		}
		response := activeReasoningFixture(family, n, n <= 2)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			marker := `data: {"type":"response.reasoning_summary_text.done"`
			if family == domain.ProviderAnthropic {
				marker = `data: {"type":"content_block_delta","index":3,"delta":{"type":"signature_delta"`
			}
			at := bytes.Index(response, []byte(marker))
			if at < 0 {
				t.Error("missing fixture boundary")
				return
			}
			_, _ = w.Write(response[:at])
			w.(http.Flusher).Flush()
			select {
			case <-release:
				_, _ = w.Write(response[at:])
			case <-r.Context().Done():
			}
		} else {
			_, _ = w.Write(response)
		}
	}))
	defer server.Close()
	cfg := reasoningFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("effort-fixture")}})
	cwd := t.TempDir()
	writeAppTestFile(t, filepath.Join(cwd, "README.md"), []byte("read exactly once"), 0600)
	skillPath := filepath.Join(cwd, ".easycode", "skills", "effort", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0700); err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, skillPath, []byte("---\nname: effort\ndescription: same name skill\n---\nsame-name-effort-marker\n"), 0600)
	resources := mustOpenChatResources(t, ctx, cfg, filepath.Join(t.TempDir(), "sessions"), "", cwd)
	defer resources.close(context.Background())
	control, err := startTUIControlSession(ctx, resources.runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Shutdown(context.Background())
	inner := tui.NewModel(ctx, "effort-smoke", control).WithInferenceSettings(resources.inference).WithSkills(resources.skills).WithMarkdownColor(os.Getenv("NO_COLOR") == "")
	program := tea.NewProgram(effortTerminalModel{inner: inner, ack: ack, release: release, once: once}, tea.WithContext(ctx), tea.WithoutSignalHandler())
	writeEffortTerminalAck(ack, effortTerminalAck{Kind: "ready"})
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	writeEffortTerminalAck(ack, effortTerminalAck{Kind: "total", Number: int(requests.Load())})
}
